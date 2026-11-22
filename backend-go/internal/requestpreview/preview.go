// Package requestpreview exercises the production builders without scheduling,
// sending HTTP, resolving OAuth credentials, or mutating saved configuration.
package requestpreview

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/converters"
	"github.com/soulteary/yunqi/internal/handlers/chat"
	"github.com/soulteary/yunqi/internal/handlers/gemini"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/providers"
	"github.com/soulteary/yunqi/internal/redaction"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/tidwall/sjson"
	"time"
)

const MaxSampleBytes = 32768

// Only builder options may be overridden. Identity, keys, quotas and runtime state
// cannot be replaced by a preview draft.
var optionFields = []string{"serviceType", "authHeader", "customHeaders", "reasoningParamStyle", "textVerbosity", "fastMode", "codexToolCompat", "stripCodexClientTools", "normalizeMetadataUserId", "stripBillingHeader", "normalizeSystemRoleToTopLevel", "injectDummyThoughtSignature", "stripThoughtSignature", "responsesClaudePrefixCache"}

type Input struct {
	Protocol string                     `json:"protocol"`
	Model    string                     `json:"model"`
	Stream   bool                       `json:"stream"`
	Body     json.RawMessage            `json:"body"`
	Draft    map[string]json.RawMessage `json:"draft,omitempty"`
}
type Effect struct {
	Field  string `json:"field"`
	Status string `json:"status"`
}
type Result struct {
	Conversion     converters.ConversionReport `json:"conversion"`
	Blocked        bool                        `json:"blocked"`
	Mode           string                      `json:"mode"`
	SourceProtocol string                      `json:"sourceProtocol"`
	TargetProtocol string                      `json:"targetProtocol"`
	Method         string                      `json:"method"`
	URL            string                      `json:"url"`
	Headers        map[string]string           `json:"headers"`
	Body           json.RawMessage             `json:"body,omitempty"`
	BodyOmitted    bool                        `json:"bodyOmitted"`
	Effects        []Effect                    `json:"effects"`
	Boundaries     []string                    `json:"boundaries"`
}

func ApplyDraft(up config.UpstreamConfig, draft map[string]json.RawMessage) (config.UpstreamConfig, error) {
	data, _ := json.Marshal(up)
	var merged map[string]json.RawMessage
	_ = json.Unmarshal(data, &merged)
	for key, value := range draft {
		allowed := false
		for _, field := range optionFields {
			if key == field {
				allowed = true
				break
			}
		}
		if !allowed {
			return up, errors.New("draft contains a field outside request builder options")
		}
		merged[key] = value
	}
	data, _ = json.Marshal(merged)
	var decoded config.UpstreamConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		return up, errors.New("invalid draft option type")
	}
	up = decoded
	if len(up.CustomHeaders) > 64 {
		return up, errors.New("preview supports at most 64 custom headers")
	}
	headerBytes := 0
	for name, value := range up.CustomHeaders {
		headerBytes += len(name) + len(value)
		if len(name) > 128 || len(value) > 4096 {
			return up, errors.New("preview custom header limit exceeded")
		}
	}
	if headerBytes > 32768 {
		return up, errors.New("preview custom header limit exceeded")
	}
	switch up.ServiceType {
	case "openai", "claude", "responses", "gemini", "copilot":
	default:
		return up, errors.New("unsupported service type")
	}
	return up, nil
}

func Build(up config.UpstreamConfig, input Input) (Result, error) {
	result := Result{SourceProtocol: input.Protocol, Effects: []Effect{}, Boundaries: []string{"single sample; fields marked no_effect_for_sample may affect other inputs", "no scheduling, automatic model/effort selection, learned compatibility, session history, transport or upstream validation", "first configured endpoint and key; no network or OAuth refresh"}}
	if len(input.Body) == 0 || len(input.Body) > MaxSampleBytes || !json.Valid(input.Body) {
		return result, errors.New("sample must be valid JSON within 32768 bytes")
	}
	var sample map[string]json.RawMessage
	if json.Unmarshal(input.Body, &sample) != nil || sample == nil {
		return result, errors.New("sample must be a JSON object")
	}
	if previous, exists := sample["previous_response_id"]; exists && string(previous) != "null" && string(previous) != `""` {
		return result, errors.New("offline preview cannot read or create conversation history; remove previous_response_id")
	}
	var err error
	up, err = ApplyDraft(up, input.Draft)
	if err != nil {
		return result, err
	}
	if up.ServiceType == "copilot" {
		return result, errors.New("offline preview cannot resolve Copilot OAuth endpoints; no credentials were refreshed or sent")
	}
	if up.ModelImportEnabled() {
		actual, importErr := up.CheckImportedModel(input.Model)
		if importErr != nil {
			return result, importErr
		}
		input.Model = actual
		if input.Protocol != "gemini" {
			input.Body, err = sjson.SetBytes(input.Body, "model", actual)
			if err != nil {
				return result, err
			}
		}
	}
	runtime := config.RuntimeUpstreamForAutoManagedProvider(&up)
	key := "preview-no-key"
	if len(runtime.APIKeys) > 0 {
		key = runtime.APIKeys[0]
	}
	// The registry already observes persisted config. Draft custom values are only
	// masked in this result, never retained globally from an untrusted sample.
	req, body, err := build(runtime, input, key)
	if err != nil {
		return result, err
	}
	result.TargetProtocol = targetProtocol(runtime.ServiceType)
	result.Conversion, err = converters.AnalyzeConversion(input.Body, body, input.Protocol, result.TargetProtocol)
	result.Blocked = err != nil && input.Protocol != result.TargetProtocol
	result.Mode = "conversion"
	if input.Protocol == result.TargetProtocol {
		result.Mode = "native_with_compatibility"
	}
	result.Method = req.Method
	result.URL = safeURL(req.URL, key)
	result.Headers = map[string]string{}
	for name, values := range req.Header {
		value := strings.Join(values, ", ")
		lower := strings.ToLower(name)
		sensitive := strings.Contains(lower, "auth") || strings.Contains(lower, "credential") || strings.Contains(lower, "cookie") || strings.HasSuffix(lower, "-key") || strings.HasSuffix(lower, "-token") || strings.HasSuffix(lower, "-secret")
		for custom := range up.CustomHeaders {
			if strings.EqualFold(custom, name) {
				sensitive = true
			}
		}
		if sensitive {
			value = redaction.Mask
		} else {
			value = redaction.String(value)
		}
		result.Headers[name] = value
	}
	safe := sanitizeBody(body, up, key)
	// Explicitly suppress the entire body if the process cannot safely redact it.
	if redaction.RegistryStats().FailClosed || len(safe) > 128*1024 {
		result.BodyOmitted = true
	} else {
		result.Body = json.RawMessage(safe)
	}
	original := reflect.ValueOf(up)
	active := reflect.ValueOf(*runtime)
	typ := original.Type()
	byJSON := map[string]int{}
	for i := 0; i < typ.NumField(); i++ {
		byJSON[strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]] = i
	}
	for _, field := range optionFields {
		index, ok := byJSON[field]
		if !ok || field == "serviceType" {
			continue
		}
		configured := original.Field(index)
		effective := active.Field(index)
		status := "no_effect_for_sample"
		if !reflect.DeepEqual(configured.Interface(), effective.Interface()) {
			status = "overridden_by_managed_policy"
		} else {
			baseline := *runtime
			v := reflect.ValueOf(&baseline).Elem().Field(index)
			if !v.IsZero() {
				v.SetZero()
				other, otherBody, buildErr := build(&baseline, input, key)
				if buildErr != nil || !sameRequest(req, body, other, otherBody) {
					status = "applied_to_sample"
				}
			} else {
				status = "default_or_unset"
			}
		}
		result.Effects = append(result.Effects, Effect{field, status})
	}
	sort.Slice(result.Effects, func(i, j int) bool { return result.Effects[i].Field < result.Effects[j].Field })
	return result, nil
}
func targetProtocol(service string) string {
	switch service {
	case "claude":
		return "messages"
	case "openai", "":
		return "chat"
	default:
		return service
	}
}
func sameRequest(a *http.Request, ab []byte, b *http.Request, bb []byte) bool {
	return b != nil && a.Method == b.Method && a.URL.String() == b.URL.String() && reflect.DeepEqual(a.Header, b.Header) && bytes.Equal(ab, bb)
}
func safeURL(u *url.URL, key string) string {
	copy := *u
	copy.User = nil
	q := copy.Query()
	for name := range q {
		q.Set(name, redaction.Mask)
	}
	copy.RawQuery = q.Encode()
	value := copy.String()
	if key != "" {
		value = strings.ReplaceAll(value, key, redaction.Mask)
		value = strings.ReplaceAll(value, url.QueryEscape(key), redaction.Mask)
	}
	return redaction.String(value)
}

type discardWriter struct{ header http.Header }

func (w *discardWriter) Header() http.Header            { return w.header }
func (w *discardWriter) Write(data []byte) (int, error) { return len(data), nil }
func (w *discardWriter) WriteHeader(int)                {}
func build(up *config.UpstreamConfig, input Input, key string) (*http.Request, []byte, error) {
	path := "/v1/" + input.Protocol
	switch input.Protocol {
	case "chat":
		path = "/v1/chat/completions"
	case "messages", "responses":
	case "gemini":
		path = "/v1beta/models/" + url.PathEscape(input.Model) + ":generateContent"
	default:
		return nil, nil, errors.New("preview supports messages/chat/responses/gemini")
	}
	inbound, _ := http.NewRequest(http.MethodPost, "http://preview.invalid"+path, bytes.NewReader(input.Body))
	inbound.Header.Set("Content-Type", "application/json")
	c, app := httpbridge.NewContext(&discardWriter{http.Header{}}, inbound)
	defer app.ReleaseCtx(c)
	var req *http.Request
	var err error
	switch input.Protocol {
	case "chat":
		req, err = chat.BuildRequestPreview(c, up, key, input.Body, input.Model, input.Stream)
	case "gemini":
		req, err = gemini.BuildRequestPreview(c, up, key, input.Body, input.Model, input.Stream)
	case "responses":
		req, _, err = (&providers.ResponsesProvider{SessionManager: session.NewEphemeralSessionManager(time.Hour, 100, 100000)}).ConvertBodyToProviderRequest(c, up, key, input.Body, path)
	case "messages":
		var provider providers.Provider
		if up.ServiceType == "responses" {
			provider = &providers.ResponsesProvider{SessionManager: session.NewEphemeralSessionManager(time.Hour, 100, 100000)}
		} else {
			provider = providers.GetProvider(up.ServiceType)
		}
		if provider == nil {
			return nil, nil, errors.New("unsupported provider")
		}
		req, _, err = provider.ConvertToProviderRequest(c, up, key)
	}
	if err != nil {
		return nil, nil, err
	}
	if req == nil || req.Body == nil {
		return nil, nil, errors.New("builder returned no request body")
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, 1024*1024+1))
	_ = req.Body.Close()
	if err != nil {
		return nil, nil, err
	}
	if len(body) > 1024*1024 {
		return nil, nil, errors.New("built request exceeds preview limit")
	}
	return req, body, nil
}

// A draft may contain credentials which have never entered the persisted config
// registry. Mask them locally without teaching the global registry attacker data.
func sanitizeBody(body []byte, up config.UpstreamConfig, key string) []byte {
	var value any
	if json.Unmarshal(body, &value) != nil {
		return []byte(`{"preview":"body omitted: invalid JSON"}`)
	}
	values := []string{}
	add := func(secret string) {
		if secret == "" {
			return
		}
		values = append(values, secret, url.QueryEscape(secret), url.PathEscape(secret))
		encoded, _ := json.Marshal(secret)
		if len(encoded) >= 2 {
			values = append(values, string(encoded[1:len(encoded)-1]))
		}
	}
	add(key)
	for _, v := range up.CustomHeaders {
		if v != "" {
			add(v)
		}
	}
	sort.SliceStable(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	var scrub func(any) any
	scrub = func(v any) any {
		switch x := v.(type) {
		case string:
			for _, secret := range values {
				if secret != "" {
					x = strings.ReplaceAll(x, secret, redaction.Mask)
				}
			}
			return x
		case map[string]any:
			for k, item := range x {
				x[k] = scrub(item)
			}
			return x
		case []any:
			for i, item := range x {
				x[i] = scrub(item)
			}
			return x
		default:
			return v
		}
	}
	data, _ := json.Marshal(scrub(value))
	return redaction.JSON(data)
}
