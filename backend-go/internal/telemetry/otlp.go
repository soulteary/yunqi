package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/soulteary/yunqi/internal/redaction"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type OTLPConfig struct {
	Enabled               bool
	Endpoint, ServiceName string
	SampleRatio           float64
	PropagateUpstream     bool
}

func OTLPConfigFromEnv() (OTLPConfig, error) {
	cfg := OTLPConfig{ServiceName: "yunqi", SampleRatio: .1, PropagateUpstream: true}
	var err error
	if raw := os.Getenv("YUNQI_OTLP_ENABLED"); raw != "" {
		cfg.Enabled, err = strconv.ParseBool(raw)
		if err != nil {
			return cfg, errors.New("invalid YUNQI_OTLP_ENABLED")
		}
	}
	if !cfg.Enabled {
		return cfg, nil
	}
	cfg.Endpoint = os.Getenv("YUNQI_OTLP_ENDPOINT")
	if cfg.Endpoint == "" {
		cfg.Endpoint = os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = strings.TrimRight(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"), "/")
		if cfg.Endpoint != "" {
			cfg.Endpoint += "/v1/traces"
		}
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "http://127.0.0.1:4318/v1/traces"
	}
	if name := os.Getenv("OTEL_SERVICE_NAME"); name != "" {
		cfg.ServiceName = name
	}
	if raw := os.Getenv("YUNQI_OTLP_SAMPLE_RATIO"); raw != "" {
		cfg.SampleRatio, err = strconv.ParseFloat(raw, 64)
		if err != nil {
			return cfg, errors.New("invalid OTLP sample ratio")
		}
	}
	if raw := os.Getenv("YUNQI_OTLP_PROPAGATE_UPSTREAM"); raw != "" {
		cfg.PropagateUpstream, err = strconv.ParseBool(raw)
		if err != nil {
			return cfg, errors.New("invalid OTLP propagation flag")
		}
	}
	return cfg, validateOTLPConfig(cfg)
}
func validateOTLPConfig(cfg OTLPConfig) error {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("OTLP requires an HTTP(S) endpoint without userinfo, query or fragment")
	}
	if math.IsNaN(cfg.SampleRatio) || math.IsInf(cfg.SampleRatio, 0) || cfg.SampleRatio < 0 || cfg.SampleRatio > 1 {
		return errors.New("OTLP sample ratio must be between 0 and 1")
	}
	if cfg.ServiceName == "" || len(cfg.ServiceName) > 128 {
		return errors.New("invalid OTLP service name")
	}
	for _, r := range cfg.ServiceName {
		if unicode.IsControl(r) {
			return errors.New("invalid OTLP service name")
		}
	}
	return nil
}

type tracingContextKey struct{}
type Tracing struct {
	provider           *sdktrace.TracerProvider
	tracer             trace.Tracer
	propagate          bool
	failures, exported atomic.Uint64
}
type safeSpanExporter struct {
	base  sdktrace.SpanExporter
	state *Tracing
}

func (e *safeSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if err := e.base.ExportSpans(ctx, spans); err != nil {
		e.state.failures.Add(1)
		return errors.New("OTLP export failed")
	}
	e.state.exported.Add(uint64(len(spans)))
	return nil
}
func (e *safeSpanExporter) Shutdown(ctx context.Context) error {
	if err := e.base.Shutdown(ctx); err != nil {
		return errors.New("OTLP exporter shutdown failed")
	}
	return nil
}

// ConfigureTracing uses an explicitly supplied policy-protected HTTP client. It
// does not install global providers or instrument its own exporter transport.
func (r *Registry) ConfigureTracing(ctx context.Context, cfg OTLPConfig, client *http.Client) error {
	if !cfg.Enabled {
		return nil
	}
	if err := validateOTLPConfig(cfg); err != nil {
		return err
	}
	if client == nil {
		return errors.New("OTLP HTTP client is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tracing != nil {
		return errors.New("OTLP already configured")
	}
	bounded := *client
	if bounded.Timeout <= 0 || bounded.Timeout > 2*time.Second {
		bounded.Timeout = 2 * time.Second
	}
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	// Protect collector authentication before SDK configuration can report errors.
	for _, env := range []string{"OTEL_EXPORTER_OTLP_HEADERS", "OTEL_EXPORTER_OTLP_TRACES_HEADERS"} {
		for _, part := range strings.Split(os.Getenv(env), ",") {
			if _, value, ok := strings.Cut(part, "="); ok {
				value = strings.TrimSpace(value)
				values := []string{value}
				if decoded, err := url.PathUnescape(value); err == nil {
					values = append(values, decoded)
				}
				redaction.Add(values)
			}
		}
	}
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.Endpoint), otlptracehttp.WithEncoding(otlptracehttp.EncodingProtobuf), otlptracehttp.WithHTTPClient(&bounded), otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}))
	if err != nil {
		return errors.New("cannot configure OTLP exporter")
	}
	state := &Tracing{propagate: cfg.PropagateUpstream}
	state.provider = sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SampleRatio)), sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", cfg.ServiceName))), sdktrace.WithSpanLimits(sdktrace.SpanLimits{AttributeCountLimit: 32, AttributeValueLengthLimit: 128, EventCountLimit: 32, LinkCountLimit: 8, AttributePerEventCountLimit: 8, AttributePerLinkCountLimit: 8}), sdktrace.WithBatcher(&safeSpanExporter{exporter, state}, sdktrace.WithMaxQueueSize(512), sdktrace.WithMaxExportBatchSize(64), sdktrace.WithBatchTimeout(time.Second), sdktrace.WithExportTimeout(2*time.Second)))
	state.tracer = state.provider.Tracer("github.com/soulteary/yunqi")
	r.tracing = state
	return nil
}
func (r *Registry) ShutdownTracing(ctx context.Context) error {
	r.mu.Lock()
	state := r.tracing
	r.mu.Unlock()
	if state == nil {
		return nil
	}
	if err := state.provider.Shutdown(ctx); err != nil {
		return errors.New("OTLP shutdown incomplete")
	}
	return nil
}
func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if _, ok := ctx.Value(tracingContextKey{}).(*Tracing); !ok {
		return ""
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
func safeID(value string) string {
	if value == "" || len(value) > 128 || redaction.String(value) != value {
		return ""
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_.:-", r)) {
			return ""
		}
	}
	return value
}
func LinkInternal(ctx context.Context, correlationID, internalTraceUID string) {
	if ctx == nil {
		return
	}
	DiagnosticsFromContext(ctx).link(correlationID, internalTraceUID)
	span := trace.SpanFromContext(ctx)
	if id := safeID(correlationID); id != "" {
		span.SetAttributes(attribute.String("yunqi.request.correlation_id", id))
	}
	if id := safeID(internalTraceUID); id != "" {
		span.SetAttributes(attribute.String("yunqi.autopilot.trace_uid", id))
		span.AddEvent("autopilot.trace", trace.WithAttributes(attribute.String("yunqi.autopilot.trace_uid", id)))
	}
}
func (r *Registry) startServerTrace(ctx context.Context, headers http.Header, endpoint, method string) (context.Context, func(int, *PhaseSnapshot)) {
	r.mu.Lock()
	state := r.tracing
	r.mu.Unlock()
	if state == nil {
		return ctx, func(int, *PhaseSnapshot) {}
	}
	if !trace.SpanContextFromContext(ctx).IsValid() {
		incoming := propagation.TraceContext{}.Extract(context.Background(), propagation.HeaderCarrier(headers))
		sc := trace.SpanContextFromContext(incoming)
		if sc.IsValid() {
			ctx = trace.ContextWithRemoteSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{TraceID: sc.TraceID(), SpanID: sc.SpanID(), TraceFlags: sc.TraceFlags(), Remote: true}))
		}
	}
	ctx, span := state.tracer.Start(ctx, "yunqi."+endpoint, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("yunqi.endpoint", endpoint), attribute.String("http.request.method", method)))
	ctx = context.WithValue(ctx, tracingContextKey{}, state)
	return ctx, func(status int, phases *PhaseSnapshot) {
		span.SetAttributes(attribute.Int("http.response.status_code", status))
		if status >= 500 {
			span.SetStatus(codes.Error, "server_error")
		}
		if ctx.Err() != nil {
			span.SetAttributes(attribute.Bool("yunqi.client_canceled", true))
		}
		if phases != nil {
			span.SetAttributes(attribute.Float64("yunqi.duration_ms", phases.TotalMs))
			for name, value := range phases.StagesMs {
				span.SetAttributes(attribute.Float64("yunqi.phase_ms."+name, value))
			}
		}
		span.End()
	}
}
func traceEvent(ctx context.Context, event, outcome string) {
	if ctx == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	span.AddEvent(event, trace.WithAttributes(attribute.String("outcome", outcome)))
	if event == "sse_terminal" {
		span.SetAttributes(attribute.String("yunqi.stream.terminal", outcome))
		if outcome == "error" || outcome == "stalled" || outcome == "failed" {
			span.SetStatus(codes.Error, "stream_failed")
		}
	}
}
func StartUpstreamTrace(req *http.Request, protocol string) (*http.Request, func(*http.Response, error)) {
	if req == nil {
		return req, func(*http.Response, error) {}
	}
	state, _ := req.Context().Value(tracingContextKey{}).(*Tracing)
	if state == nil {
		return req, func(*http.Response, error) {}
	}
	switch protocol {
	case "openai", "claude", "responses", "gemini", "copilot":
	default:
		protocol = "other"
	}
	parentSpan := trace.SpanFromContext(req.Context())
	ctx, span := state.tracer.Start(req.Context(), "yunqi.upstream."+protocol, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("yunqi.upstream.protocol", protocol)))
	req = req.Clone(ctx)
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Del("traceparent")
	req.Header.Del("tracestate")
	req.Header.Del("baggage")
	if state.propagate {
		propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(req.Header))
	}
	var once sync.Once
	finish := func(err error) {
		once.Do(func() {
			if err != nil && err != io.EOF {
				span.SetStatus(codes.Error, "upstream_request_failed")
			}
			span.End()
		})
	}
	return req, func(resp *http.Response, err error) {
		if err != nil {
			finish(err)
			return
		}
		if resp == nil {
			finish(errors.New("missing upstream response"))
			return
		}
		span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
		if resp.StatusCode >= 400 {
			span.SetStatus(codes.Error, "upstream_http_error")
		}
		for _, header := range []string{"x-request-id", "request-id", "anthropic-request-id", "x-amzn-requestid", "x-goog-request-id"} {
			if id := safeID(resp.Header.Get(header)); id != "" {
				span.SetAttributes(attribute.String("yunqi.upstream.request_id", id))
				parentSpan.SetAttributes(attribute.String("yunqi.upstream.request_id", id))
				parentSpan.AddEvent("upstream.response", trace.WithAttributes(attribute.String("yunqi.upstream.request_id", id), attribute.Int("http.response.status_code", resp.StatusCode)))
				break
			}
		}
		if resp.Body == nil {
			finish(nil)
		} else {
			resp.Body = &tracedResponseBody{ReadCloser: resp.Body, finish: finish}
		}
	}
}

type tracedResponseBody struct {
	io.ReadCloser
	finish func(error)
}

func (b *tracedResponseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.finish(err)
	}
	return n, err
}
func (b *tracedResponseBody) Close() error { err := b.ReadCloser.Close(); b.finish(err); return err }

func writeTracingHealth(w io.Writer, state *Tracing) {
	enabled := 0
	var failures, exported uint64
	if state != nil {
		enabled = 1
		failures = state.failures.Load()
		exported = state.exported.Load()
	}
	_, _ = fmt.Fprintf(w, "# HELP yunqi_otel_enabled OTLP tracing is configured.\n# TYPE yunqi_otel_enabled gauge\nyunqi_otel_enabled %d\n# HELP yunqi_otel_export_failures_total Failed export batches.\n# TYPE yunqi_otel_export_failures_total counter\nyunqi_otel_export_failures_total %d\n# HELP yunqi_otel_exported_spans_total Successfully exported sampled spans.\n# TYPE yunqi_otel_exported_spans_total counter\nyunqi_otel_exported_spans_total %d\n", enabled, failures, exported)
	failClosed := 0
	if redaction.RegistryStats().FailClosed {
		failClosed = 1
	}
	_, _ = fmt.Fprintf(w, "# HELP yunqi_diagnostic_redaction_fail_closed Diagnostic redaction registry exceeded safe bounds.\n# TYPE yunqi_diagnostic_redaction_fail_closed gauge\nyunqi_diagnostic_redaction_fail_closed %d\n", failClosed)
}
