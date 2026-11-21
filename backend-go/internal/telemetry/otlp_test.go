package telemetry_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/redaction"
	"github.com/soulteary/yunqi/internal/telemetry"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestOTLPRealExportPropagationAndPrivacy(t *testing.T) {
	redaction.Configure([]string{"gateway-secret", "upstream-secret"})
	defer redaction.Configure(nil)
	var mu sync.Mutex
	var spans []*tracepb.Span
	var exported []byte
	collectorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			t.Errorf("OTLP path %s", r.URL.Path)
		}
		var reader io.Reader = r.Body
		if r.Header.Get("Content-Encoding") == "gzip" {
			gz, err := gzip.NewReader(r.Body)
			if err != nil {
				t.Error(err)
				return
			}
			defer gz.Close()
			reader = gz
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Error(err)
			return
		}
		var request collector.ExportTraceServiceRequest
		if err := proto.Unmarshal(body, &request); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		exported = append(exported, body...)
		for _, resource := range request.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				spans = append(spans, scope.Spans...)
			}
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	}))
	defer collectorServer.Close()
	incomingTraceID := "0123456789abcdef0123456789abcdef"
	var upstreamTraceparent string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte("private-prompt")) {
			t.Error("test prompt did not traverse upstream")
		}
		upstreamTraceparent = r.Header.Get("traceparent")
		if r.Header.Get("tracestate") != "" || r.Header.Get("baggage") != "" {
			t.Error("private propagation headers retained")
		}
		w.Header().Set("X-Request-Id", "req_provider_xyz")
		_, _ = io.WriteString(w, `{"output":"private-output","usage":{"input_tokens":2}}`)
	}))
	defer upstream.Close()
	registry := telemetry.NewRegistry()
	if err := registry.ConfigureTracing(context.Background(), telemetry.OTLPConfig{Enabled: true, Endpoint: collectorServer.URL + "/v1/traces", ServiceName: "yunqi-test", SampleRatio: 1, PropagateUpstream: true}, collectorServer.Client()); err != nil {
		t.Fatal(err)
	}
	app := httpbridge.NewApp()
	app.Use(registry.Middleware())
	app.Post("/v1/messages", func(c fiber.Ctx) error {
		ctx := httpbridge.Request(c).Context()
		telemetry.LinkInternal(ctx, "srv_correlation", "rt_internal")
		body, _ := io.ReadAll(httpbridge.Request(c).Body)
		req, _ := http.NewRequestWithContext(ctx, "POST", upstream.URL, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer upstream-secret")
		req.Header.Set("tracestate", "foo=private-tracestate")
		req.Header.Set("baggage", "secret=private-baggage")
		resp, err := common.SendRequestWithLifecycleTrace(req, &config.UpstreamConfig{ServiceType: "openai"}, &config.EnvConfig{RequestTimeout: 5000, ResponseHeaderTimeout: 5}, false, "Messages", nil)
		if err != nil {
			return err
		}
		reply, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		httpbridge.Data(c, 200, "application/json", reply)
		return nil
	})
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"private-model","messages":[{"content":"private-prompt"}]}`))
	req.Header.Set("Authorization", "Bearer gateway-secret")
	req.Header.Set("traceparent", "00-"+incomingTraceID+"-1111111111111111-01")
	req.Header.Set("tracestate", "foo=private-tracestate")
	req.Header.Set("baggage", "secret=private-baggage")
	w := httptest.NewRecorder()
	httpbridge.Handler(app).ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w.Header().Get("X-Yunqi-Trace-Id") != incomingTraceID {
		t.Fatal("missing gateway trace link")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := registry.ShutdownTracing(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(spans) != 2 {
		t.Fatalf("spans=%d", len(spans))
	}
	for _, secret := range []string{"private-prompt", "private-output", "private-model", "gateway-secret", "upstream-secret", "private-baggage", "private-tracestate", upstream.URL} {
		if bytes.Contains(exported, []byte(secret)) {
			t.Fatalf("export leaked %s", secret)
		}
	}
	if !strings.Contains(upstreamTraceparent, incomingTraceID) {
		t.Fatal("upstream trace context missing")
	}
	foundLink, foundProvider := false, false
	var serverSpan, clientSpan *tracepb.Span
	for _, span := range spans {
		if span.Kind == tracepb.Span_SPAN_KIND_SERVER {
			serverSpan = span
		} else if span.Kind == tracepb.Span_SPAN_KIND_CLIENT {
			clientSpan = span
		}
		for _, attr := range span.Attributes {
			if attr.Key == "yunqi.autopilot.trace_uid" && attr.Value.GetStringValue() == "rt_internal" {
				foundLink = true
			}
			if attr.Key == "yunqi.upstream.request_id" && attr.Value.GetStringValue() == "req_provider_xyz" {
				foundProvider = true
			}
		}
	}
	if !foundLink || !foundProvider || serverSpan == nil || clientSpan == nil || !bytes.Equal(clientSpan.ParentSpanId, serverSpan.SpanId) {
		t.Fatal("missing trace linkage")
	}
}
func TestOTLPDisabledAndZeroSamplingDoNotExport(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer server.Close()
	for _, enabled := range []bool{false, true} {
		registry := telemetry.NewRegistry()
		if err := registry.ConfigureTracing(context.Background(), telemetry.OTLPConfig{Enabled: enabled, Endpoint: server.URL + "/v1/traces", ServiceName: "test", SampleRatio: 0}, server.Client()); err != nil {
			t.Fatal(err)
		}
		app := httpbridge.NewApp()
		app.Use(registry.Middleware())
		app.Post("/v1/messages", func(c fiber.Ctx) error { httpbridge.Text(c, 200, "ok"); return nil })
		httpbridge.Handler(app).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", nil))
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := registry.ShutdownTracing(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("disabled/zero sampling exported")
	}
}
func TestOTLPConfigRejectsUnsafeOrInvalidSettings(t *testing.T) {
	t.Setenv("YUNQI_OTLP_ENABLED", "false")
	t.Setenv("YUNQI_OTLP_ENDPOINT", "http://user:secret@collector/")
	if cfg, err := telemetry.OTLPConfigFromEnv(); err != nil || cfg.Enabled {
		t.Fatal("disabled default unexpectedly activated")
	}
	t.Setenv("YUNQI_OTLP_ENABLED", "true")
	for _, endpoint := range []string{"http://user:secret@collector/", "http://collector/v1/traces?token=secret", "ftp://collector/"} {
		t.Setenv("YUNQI_OTLP_ENDPOINT", endpoint)
		if _, err := telemetry.OTLPConfigFromEnv(); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("unsafe configuration accepted or leaked")
		}
	}
	t.Setenv("YUNQI_OTLP_ENDPOINT", "http://127.0.0.1:4318/v1/traces")
	for _, ratio := range []string{"NaN", "Inf", "-1", "1.1"} {
		t.Setenv("YUNQI_OTLP_SAMPLE_RATIO", ratio)
		if _, err := telemetry.OTLPConfigFromEnv(); err == nil {
			t.Fatal("invalid sample ratio", ratio)
		}
	}
}
func TestOTLPExporterFailureDoesNotBlockHandlerAndShutdownIsBounded(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	client := server.Client()
	client.Timeout = 50 * time.Millisecond
	registry := telemetry.NewRegistry()
	if err := registry.ConfigureTracing(context.Background(), telemetry.OTLPConfig{Enabled: true, Endpoint: server.URL + "/v1/traces", ServiceName: "test", SampleRatio: 1}, client); err != nil {
		t.Fatal(err)
	}
	app := httpbridge.NewApp()
	app.Use(registry.Middleware())
	app.Post("/v1/messages", func(c fiber.Ctx) error { httpbridge.Text(c, 200, "ok"); return nil })
	w := httptest.NewRecorder()
	httpbridge.Handler(app).ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = registry.ShutdownTracing(ctx)
	select {
	case <-entered:
	default:
		t.Fatal("export path was not exercised")
	}
	if ctx.Err() != nil {
		t.Fatal("shutdown exceeded bounded exporter timeout")
	}
}
func TestOTLPPropagationCanBeDisabledWithoutForwardingUntrustedState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("zero sample exporter contacted collector") }))
	defer server.Close()
	registry := telemetry.NewRegistry()
	if err := registry.ConfigureTracing(context.Background(), telemetry.OTLPConfig{Enabled: true, Endpoint: server.URL + "/v1/traces", ServiceName: "test", SampleRatio: 0, PropagateUpstream: false}, server.Client()); err != nil {
		t.Fatal(err)
	}
	app := httpbridge.NewApp()
	app.Use(registry.Middleware())
	app.Post("/v1/messages", func(c fiber.Ctx) error {
		req, _ := http.NewRequestWithContext(httpbridge.Request(c).Context(), "POST", "https://provider.invalid/", nil)
		req.Header.Set("traceparent", "old")
		req.Header.Set("tracestate", "secret")
		req.Header.Set("baggage", "secret")
		req, done := telemetry.StartUpstreamTrace(req, "openai")
		if req.Header.Get("traceparent") != "" || req.Header.Get("tracestate") != "" || req.Header.Get("baggage") != "" {
			t.Error("untrusted propagation was retained")
		}
		done(&http.Response{StatusCode: 200, Body: http.NoBody}, nil)
		httpbridge.Text(c, 200, "ok")
		return nil
	})
	httpbridge.Handler(app).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", nil))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := registry.ShutdownTracing(ctx); err != nil {
		t.Fatal(err)
	}
}
