package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/soulteary/yunqi/internal/httpclient"
)

// ModelDetails retains the API's explicit endpoint evidence. A nil endpoint list
// means the model predates endpoint metadata; an empty list means none are allowed.
type ModelDetails struct {
	ID                 string   `json:"id"`
	SupportedEndpoints []string `json:"supported_endpoints"`
}

func (m *ModelDetails) UnmarshalJSON(body []byte) error {
	var raw struct {
		ID        string          `json:"id"`
		Endpoints json.RawMessage `json:"supported_endpoints"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}
	m.ID = raw.ID
	m.SupportedEndpoints = nil
	if len(raw.Endpoints) > 0 && string(raw.Endpoints) != "null" {
		m.SupportedEndpoints = []string{}
		// Malformed explicit evidence is not the same as absent legacy metadata.
		if err := json.Unmarshal(raw.Endpoints, &m.SupportedEndpoints); err != nil {
			m.SupportedEndpoints = []string{}
		}
	}
	return nil
}

func ParseModelDetails(body []byte) []ModelDetails {
	var response struct {
		Data []ModelDetails `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil {
		return nil
	}
	return response.Data
}

func EndpointProtocol(endpoint string) string {
	switch strings.TrimSpace(endpoint) {
	case "/responses", "/v1/responses":
		return "responses"
	case "/chat/completions", "/v1/chat/completions":
		return "chat"
	case "/messages", "/v1/messages":
		return "messages"
	default:
		return ""
	}
}

// SelectEndpoint never infers a protocol from a model name or tries an endpoint
// excluded by explicit metadata. WebSocket-only endpoints are not HTTP endpoints.
func SelectEndpoint(models []ModelDetails, model, preferred string) (string, string, error) {
	if preferred != "" && preferred != "responses" && preferred != "chat" && preferred != "messages" {
		return "", "", fmt.Errorf("unsupported Copilot protocol %q", preferred)
	}
	var details *ModelDetails
	for i := range models {
		if models[i].ID == model {
			details = &models[i]
			break
		}
	}
	if details == nil || details.SupportedEndpoints == nil {
		if preferred != "" && preferred != "responses" {
			return "", "", fmt.Errorf("Copilot model %q has no evidence for %s", model, preferred)
		}
		return "responses", "/responses", nil
	}
	order := []string{"responses", "chat", "messages"}
	if preferred != "" {
		order = []string{preferred}
	}
	for _, protocol := range order {
		for _, endpoint := range details.SupportedEndpoints {
			if EndpointProtocol(endpoint) == protocol {
				return protocol, strings.TrimSpace(endpoint), nil
			}
		}
	}
	return "", "", fmt.Errorf("Copilot model %q does not declare a supported HTTP endpoint for %q", model, preferred)
}

type TokenResolver func(context.Context, string, string) (string, string, error)
type modelCacheEntry struct {
	models  []ModelDetails
	expires time.Time
}

// EndpointResolver shares short-lived /models evidence across discovery and inference.
// Cache keys include both runtime token and origin, so credentials never share evidence.
type EndpointResolver struct {
	token  TokenResolver
	client *http.Client
	mu     sync.Mutex
	cache  map[[32]byte]modelCacheEntry
}

func NewEndpointResolver(token TokenResolver, client *http.Client) *EndpointResolver {
	return &EndpointResolver{token: token, client: client, cache: make(map[[32]byte]modelCacheEntry)}
}

var defaultEndpointResolver = NewEndpointResolver(ResolveTokenWithProxy, nil)

func DefaultEndpointResolver() *EndpointResolver { return defaultEndpointResolver }

func (r *EndpointResolver) Models(ctx context.Context, githubToken, proxyURL string) (string, string, []ModelDetails, error) {
	token, baseURL, err := r.token(ctx, githubToken, proxyURL)
	if err != nil {
		return "", "", nil, err
	}
	if baseURL == "" {
		baseURL = DefaultAPIBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	key := sha256.Sum256([]byte(token + "\x00" + baseURL + "\x00" + proxyURL))
	r.mu.Lock()
	entry, ok := r.cache[key]
	r.mu.Unlock()
	if ok && time.Now().Before(entry.expires) {
		return token, baseURL, CloneModelDetails(entry.models), nil
	}
	client := r.client
	if client == nil {
		client = httpclient.GetManager().NewStandardClient(10*time.Second, false, proxyURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return "", "", nil, err
	}
	ApplyRuntimeHeaders(req.Header, token)
	resp, err := client.Do(req)
	if err != nil {
		return token, baseURL, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return token, baseURL, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return token, baseURL, nil, &CatalogHTTPError{StatusCode: resp.StatusCode}
	}
	models := ParseModelDetails(body)
	if models == nil {
		return token, baseURL, nil, fmt.Errorf("Copilot models returned an invalid catalog")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.cache) >= 256 {
		clear(r.cache)
	}
	r.cache[key] = modelCacheEntry{models: models, expires: time.Now().Add(5 * time.Minute)}
	return token, baseURL, CloneModelDetails(models), nil
}

func (r *EndpointResolver) Resolve(ctx context.Context, githubToken, proxyURL, model, preferred string) (token, baseURL, protocol, endpoint string, err error) {
	token, baseURL, models, fetchErr := r.Models(ctx, githubToken, proxyURL)
	if token == "" {
		return "", "", "", "", fetchErr
	}
	// Only explicit legacy "no catalog" responses retain the old HTTP path.
	// Authentication, transient failures and invalid catalogs must not trigger inference.
	if fetchErr != nil {
		var status *CatalogHTTPError
		legacy := errors.As(fetchErr, &status) && (status.StatusCode == http.StatusNotFound || status.StatusCode == http.StatusGone)
		if !legacy || (preferred != "" && preferred != "responses") {
			return "", "", "", "", fetchErr
		}
	}
	protocol, endpoint, err = SelectEndpoint(models, model, preferred)
	return
}

// CloneModelDetails preserves nil versus explicitly empty endpoint lists.
func CloneModelDetails(models []ModelDetails) []ModelDetails {
	if models == nil {
		return nil
	}
	out := make([]ModelDetails, len(models))
	for i, model := range models {
		out[i] = model
		if model.SupportedEndpoints != nil {
			out[i].SupportedEndpoints = append([]string{}, model.SupportedEndpoints...)
		}
	}
	return out
}

type CatalogHTTPError struct{ StatusCode int }

func (e *CatalogHTTPError) Error() string {
	return fmt.Sprintf("Copilot models returned HTTP %d", e.StatusCode)
}
