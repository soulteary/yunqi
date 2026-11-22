package common

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/entrybudget"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/requestscope"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/tidwall/gjson"
)

func budgetProtocol(req *http.Request, upstream *config.UpstreamConfig) string {
	path := strings.TrimRight(strings.ToLower(req.URL.Path), "/")
	switch {
	case strings.HasSuffix(path, ":generatecontent") || strings.HasSuffix(path, ":streamgeneratecontent"):
		return "gemini"
	case strings.HasSuffix(path, "/chat/completions"):
		return "chat"
	case strings.HasSuffix(path, "/messages"):
		return "messages"
	case strings.HasSuffix(path, "/responses") || strings.HasSuffix(path, "/responses/compact"):
		return "responses"
	case strings.HasSuffix(path, "/embeddings"):
		return "vectors"
	case strings.HasSuffix(path, "/images/generations") || strings.HasSuffix(path, "/images/edits") || strings.HasSuffix(path, "/images/variations"):
		return "images"
	}
	switch upstream.ServiceType {
	case "openai":
		return "chat"
	case "claude":
		return "messages"
	case "copilot":
		return "responses"
	}
	return upstream.ServiceType
}
func budgetHasMedia(value interface{}) bool {
	switch v := value.(type) {
	case []interface{}:
		for _, item := range v {
			if budgetHasMedia(item) {
				return true
			}
		}
	case map[string]interface{}:
		for key, item := range v {
			if key == "tools" || key == "tool_choice" || key == "response_format" {
				continue
			}
			switch strings.ToLower(key) {
			case "image_url", "input_audio", "audio", "video", "inlinedata", "filedata", "modalities", "responsemodalities", "mime_type", "mimetype":
				return true
			}
			if key == "type" {
				if kind, ok := item.(string); ok && (strings.Contains(kind, "image") || strings.Contains(kind, "audio") || strings.Contains(kind, "video") || kind == "document" || kind == "input_file") {
					return true
				}
			}
			if budgetHasMedia(item) {
				return true
			}
		}
	}
	return false
}

// Hosted tools have per-call/storage/search fees outside the token price schema.
// Inspect tool declarations only: client function/custom schemas may legitimately
// contain any of these words without asking the provider to run a hosted tool.
func budgetHasUnpricedServerTools(value interface{}) bool {
	root, ok := value.(map[string]interface{})
	if !ok {
		return false
	}
	tools, _ := root["tools"].([]interface{})
	for _, item := range tools {
		tool, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		kind, _ := tool["type"].(string)
		kind = strings.ToLower(kind)
		if kind == "web_search" || strings.HasPrefix(kind, "web_search_") || kind == "file_search" || kind == "code_interpreter" || kind == "image_generation" || strings.HasPrefix(kind, "web_fetch_") || strings.HasPrefix(kind, "code_execution_") {
			return true
		}
		for key := range tool {
			switch strings.ReplaceAll(strings.ToLower(key), "_", "") {
			case "googlesearch", "googlesearchretrieval", "enterprisesearch", "enterprisewebsearch", "urlcontext", "codeexecution", "googlemaps", "retrieval":
				return true
			}
		}
	}
	return false
}
func beginEntryBudget(req *http.Request, upstream *config.UpstreamConfig, env *config.EnvConfig) (*entrybudget.Attempt, *config.ModelPricing, string, error) {
	session := entrybudget.FromContext(req.Context())
	if session == nil {
		return nil, nil, "", nil
	}
	protocol := budgetProtocol(req, upstream)
	if req.GetBody == nil {
		return nil, nil, protocol, entrybudget.ErrPricing
	}
	copy, err := req.GetBody()
	if err != nil {
		return nil, nil, protocol, entrybudget.ErrPricing
	}
	defer copy.Close()
	limit := env.MaxRequestBodySize
	if limit <= 0 || limit > 64<<20 {
		limit = 64 << 20
	}
	body, err := io.ReadAll(io.LimitReader(copy, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, nil, protocol, entrybudget.ErrPricing
	}
	var parsed interface{}
	if json.Unmarshal(body, &parsed) != nil || budgetHasMedia(parsed) || budgetHasUnpricedServerTools(parsed) || protocol == "images" {
		return nil, nil, protocol, entrybudget.ErrPricing
	}
	model := gjson.GetBytes(body, "model").String()
	if model == "" && protocol == "gemini" {
		if index := strings.LastIndex(req.URL.Path, "/models/"); index >= 0 {
			model, _, _ = strings.Cut(req.URL.Path[index+len("/models/"):], ":")
		}
	}
	if model == "" {
		return nil, nil, protocol, entrybudget.ErrPricing
	}
	var caps map[string]config.UpstreamModelCapability
	if cm := middleware.BudgetConfigManager(req.Context()); cm != nil {
		caps = cm.UpstreamModelCapabilitiesSnapshot()
	}
	resolved := config.ResolveUpstreamCapability(model, upstream, caps)
	pricing := config.SnapshotModelPricing(resolved.Capability.Pricing)
	if pricing == nil {
		return nil, nil, protocol, entrybudget.ErrPricing
	}
	output := int64(0)
	explicit := false
	for _, key := range []string{"max_output_tokens", "max_completion_tokens", "max_tokens", "generationConfig.maxOutputTokens"} {
		if value := gjson.GetBytes(body, key); value.Exists() {
			number, err := strconv.ParseInt(value.Raw, 10, 64)
			if value.Type != gjson.Number || err != nil || number < 0 || number > 1_000_000_000 {
				return nil, nil, protocol, entrybudget.ErrPricing
			}
			if number > output {
				output = number
			}
			explicit = true
		}
	}
	if protocol != "vectors" && !explicit {
		output = int64(resolved.Capability.MaxOutputTokens)
		if output <= 0 {
			return nil, nil, protocol, entrybudget.ErrPricing
		}
	}
	if protocol != "vectors" && output <= 0 {
		return nil, nil, protocol, entrybudget.ErrPricing
	}
	// A generic messages estimator omits Responses input, Gemini contents and
	// embeddings input. Reserve one token per final wire byte plus framing: this
	// deliberately conservative envelope covers every serialized input field,
	// Unicode and whitespace without claiming an exact provider tokenizer.
	input := int64(len(body)) + 64
	// The shared F47 evaluator includes all reachable tiers and the highest
	// unknown cache TTL/reasoning dimensions, using this same immutable snapshot.
	upper, complete := metrics.CalculateUsageReservationUSDWithPricing(pricing, &types.Usage{InputTokens: int(input), OutputTokens: int(output)})
	if !complete {
		return nil, nil, protocol, entrybudget.ErrPricing
	}

	amount, err := entrybudget.FromUSD(upper)
	if err != nil {
		return nil, nil, protocol, err
	}
	snapshot, err := json.Marshal(pricing)
	if err != nil {
		return nil, nil, protocol, entrybudget.ErrPricing
	}
	version := config.PricingVersionID(pricing)
	attempt, err := session.Begin(req.Context(), entrybudget.AttemptQuote{Model: model, ChannelUID: upstream.ChannelUID, PricingVersion: version, Pricing: snapshot, Amount: amount})
	return attempt, pricing, protocol, err
}

func rawBudgetCounter(raw map[string]json.RawMessage, paths ...string) (int64, bool) {
	var result int64
	found := false
	for _, path := range paths {
		current := raw
		parts := strings.Split(path, ".")
		for i, key := range parts {
			value, exists := current[key]
			if !exists {
				break
			}
			if i == len(parts)-1 {
				number, err := strconv.ParseInt(string(value), 10, 64)
				if err != nil || number < 0 || number > int64(math.MaxInt/4) {
					return 0, false
				}
				if found && result != number {
					return 0, false
				}
				result = number
				found = true
				break
			}
			var nested map[string]json.RawMessage
			if json.Unmarshal(value, &nested) != nil {
				break
			}
			current = nested
		}
	}
	return result, found
}

func rawBudgetOptionalCounter(raw map[string]json.RawMessage, paths ...string) (int64, bool) {
	n, ok := rawBudgetCounter(raw, paths...)
	if ok {
		return n, true
	}
	data, _ := json.Marshal(raw)
	for _, path := range paths {
		if gjson.GetBytes(data, path).Exists() {
			return 0, false
		}
	}
	return 0, true
}
func (r *usageCapture) billingUsage(protocol string) (*types.Usage, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stream {
		r.captureLine(r.pending)
	} else if !r.overflow {
		r.captureJSON(r.pending)
	}
	r.pending = nil
	if r.truncated || r.overflow || len(r.raw) == 0 || r.stream && !r.terminals[protocol] {
		return nil, false
	}
	var input, output, cache, creation int64
	var inputOK, outputOK bool
	countersValid := true
	if protocol == "gemini" {
		input, inputOK = rawBudgetCounter(r.raw, "promptTokenCount")
		output, outputOK = rawBudgetCounter(r.raw, "candidatesTokenCount")
		thoughts, thoughtsOK := rawBudgetOptionalCounter(r.raw, "thoughtsTokenCount")
		toolInput, toolOK := rawBudgetOptionalCounter(r.raw, "toolUsePromptTokenCount")
		// Additional tool-use input cannot be charged at the output token rate.
		if !thoughtsOK || !toolOK || toolInput > 0 {
			return nil, false
		}
		if _, exists := r.raw["totalTokenCount"]; exists {
			total, totalOK := rawBudgetCounter(r.raw, "totalTokenCount")
			if !totalOK || !inputOK || total < input {
				return nil, false
			}
			if outputOK {
				if total != input+output+thoughts {
					return nil, false
				}
			} else {
				if _, present := r.raw["candidatesTokenCount"]; present {
					return nil, false
				}
				output = total - input - thoughts
				if output < 0 {
					return nil, false
				}
				outputOK = true
			}
		}

		var cacheOK bool
		cache, cacheOK = rawBudgetOptionalCounter(r.raw, "cachedContentTokenCount")
		countersValid = countersValid && cacheOK
	} else {
		input, inputOK = rawBudgetCounter(r.raw, "input_tokens", "prompt_tokens")
		output, outputOK = rawBudgetCounter(r.raw, "output_tokens", "completion_tokens")
		var cacheOK, createOK bool
		cache, cacheOK = rawBudgetOptionalCounter(r.raw, "cache_read_input_tokens", "input_tokens_details.cached_tokens", "prompt_tokens_details.cached_tokens")
		creation, createOK = rawBudgetOptionalCounter(r.raw, "cache_creation_input_tokens")
		countersValid = countersValid && cacheOK && createOK
	}
	if protocol == "vectors" {
		outputOK = true
		output = 0
	}
	if !inputOK || !outputOK || !countersValid {
		return nil, false
	}
	totalInput := input
	if protocol != "messages" {
		if cache > input {
			return nil, false
		}
		input -= cache
		if creation > 0 {
			return nil, false
		}
	}
	raw, _ := json.Marshal(r.raw)
	usage := &types.Usage{InputTokens: int(input), OutputTokens: int(output), CacheReadInputTokens: int(cache), CacheCreationInputTokens: int(creation), Provenance: &types.UsageProvenance{UsageSource: "provider", Protocol: protocol, RawUsage: raw}}
	if protocol != "messages" {
		usage.PromptTokensTotal = int(totalInput)
	}
	five, fiveOK := rawBudgetOptionalCounter(r.raw, "cache_creation.ephemeral_5m_input_tokens", "cache_creation_5m_input_tokens")
	hour, hourOK := rawBudgetOptionalCounter(r.raw, "cache_creation.ephemeral_1h_input_tokens", "cache_creation_1h_input_tokens")
	if !fiveOK || !hourOK {
		return nil, false
	}
	usage.CacheCreation5mInputTokens = int(five)
	usage.CacheCreation1hInputTokens = int(hour)
	return usage, true
}

type budgetResponseBody struct {
	capture  *usageCapture
	attempt  *entrybudget.Attempt
	pricing  *config.ModelPricing
	protocol string
	status   int
	once     sync.Once
	err      error
}

func (b *budgetResponseBody) Read(p []byte) (int, error) { return b.capture.Read(p) }
func (b *budgetResponseBody) Close() error {
	b.once.Do(func() {
		b.err = b.capture.ReadCloser.Close()
		if b.status < 200 || b.status >= 300 {
			zero := int64(0)
			_ = b.attempt.Complete(&zero, "upstream_rejected")
			return
		}
		usage, known := b.capture.billingUsage(b.protocol)
		if !known {
			_ = b.attempt.Complete(nil, "incomplete_or_missing_provider_usage")
			return
		}
		// Gemini canonical output is candidate-only. F47 reads raw thoughts as an
		// additional dimension; OpenAI reasoning is a subset, never charged twice.
		cost, complete := metrics.CalculateUsageCostUSDWithPricingAndStatus(b.pricing, usage)

		if !complete {
			_ = b.attempt.Complete(nil, "unpriced_provider_usage")
			return
		}
		amount, err := entrybudget.FromUSD(cost)
		if err != nil {
			_ = b.attempt.Complete(nil, "invalid_provider_usage_cost")
			return
		}
		accounting, _ := json.Marshal(struct {
			Protocol string          `json:"protocol"`
			Usage    *types.Usage    `json:"usage"`
			RawUsage json.RawMessage `json:"rawUsage"`
		}{b.protocol, usage, usage.Provenance.RawUsage})
		_ = b.attempt.CompleteAccounting(&amount, "provider_usage", "provider", accounting)
	})
	return b.err
}

func budgetConnectTrace(connected *atomic.Bool) *httptrace.ClientTrace {
	return &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { connected.Store(true) }}
}
func isEntryBudgetError(err error) bool {
	return errors.Is(err, entrybudget.ErrQuota) || errors.Is(err, entrybudget.ErrPricing) || errors.Is(err, entrybudget.ErrUnavailable) || errors.Is(err, entrybudget.ErrClosed) || errors.Is(err, entrybudget.ErrLeaseLost) || errors.Is(err, entrybudget.ErrRequestTimeout) || errors.Is(err, entrybudget.ErrShutdown)
}

// HandleEntryBudgetError renders only local budget rejections, before any upstream
// response is selected. Racing branches report errors to their common coordinator.
func EntryBudgetFailure(err error) (int, string, bool) {
	if !isEntryBudgetError(err) {
		return 0, "", false
	}
	status, code := 503, "entry_budget_unavailable"
	if errors.Is(err, entrybudget.ErrQuota) {
		status, code = 429, "entry_budget_exceeded"
	}
	if errors.Is(err, entrybudget.ErrPricing) {
		status, code = 400, "entry_budget_pricing_incomplete"
	}
	return status, code, true
}
func HandleEntryBudgetError(c fiber.Ctx, err error) bool {
	status, code, ok := EntryBudgetFailure(err)
	if !ok {
		return false
	}
	if httpbridge.Writer(c).Written() {
		return true
	}
	httpbridge.JSON(c, status, fiber.Map{"error": fiber.Map{"type": code, "message": "Request could not reserve its entry budget"}})
	return true
}

func budgetScopeCause(ctx context.Context) error {
	if session := entrybudget.FromContext(ctx); session != nil {
		cause := session.Cause()
		if cause != nil && !errors.Is(cause, requestscope.ErrClosed) {
			return cause
		}
	}
	return nil
}
func budgetBindError(req *http.Request, err error) error {
	if entrybudget.FromContext(req.Context()) != nil {
		return entrybudget.ErrClosed
	}
	return err
}
