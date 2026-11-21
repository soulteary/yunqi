package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"net/http"
	"strings"
	"sync"
	"time"
)

const MaxDiagnosticRequests = 1024
const MaxDiagnosticAttempts = 128
const MaxDiagnosticEvents = 256

type DiagnosticEvent struct {
	Sequence  int    `json:"sequence"`
	ElapsedMs int64  `json:"elapsedMs"`
	Stage     string `json:"stage"`
	Outcome   string `json:"outcome"`
	AttemptID string `json:"attemptId,omitempty"`
}

// No URLs, key material, model, free-form errors or client identity.
type DiagnosticAttempt struct {
	AttemptID         string `json:"attemptId"`
	ChannelUID        string `json:"channelUid,omitempty"`
	KeyUID            string `json:"keyUid,omitempty"`
	Protocol          string `json:"protocol,omitempty"`
	Source            string `json:"source,omitempty"`
	Status            string `json:"status"`
	ErrorSource       string `json:"errorSource,omitempty"`
	StatusCode        int    `json:"statusCode,omitempty"`
	UpstreamRequestID string `json:"upstreamRequestId,omitempty"`
	RacingRole        string `json:"racingRole,omitempty"`
	RacingStatus      string `json:"racingStatus,omitempty"`
	StartMs           int64  `json:"startMs"`
	DurationMs        int64  `json:"durationMs"`
}
type RequestDiagnostic struct {
	ID                string              `json:"id"`
	CorrelationID     string              `json:"correlationId,omitempty"`
	OTLPTraceID       string              `json:"otlpTraceId,omitempty"`
	AutopilotTraceUID string              `json:"autopilotTraceUid,omitempty"`
	Endpoint          string              `json:"endpoint"`
	StartedAt         time.Time           `json:"startedAt"`
	StatusCode        int                 `json:"statusCode"`
	TerminalOutcome   string              `json:"terminalOutcome,omitempty"`
	Completed         bool                `json:"completed"`
	Canceled          bool                `json:"canceled"`
	Attempts          []DiagnosticAttempt `json:"attempts"`
	Events            []DiagnosticEvent   `json:"events"`
	DroppedAttempts   int                 `json:"droppedAttempts"`
	DroppedEvents     int                 `json:"droppedEvents"`
	PhaseTiming       *PhaseSnapshot      `json:"phaseTiming,omitempty"`
}
type Diagnostics struct {
	mu     sync.Mutex
	data   RequestDiagnostic
	start  time.Time
	timing *Timeline
	index  map[string]int
}
type diagnosticsContextKey struct{}

func DiagnosticsFromContext(ctx context.Context) *Diagnostics {
	if ctx == nil {
		return nil
	}
	d, _ := ctx.Value(diagnosticsContextKey{}).(*Diagnostics)
	return d
}
func DiagnosticRequestID(ctx context.Context) string {
	d := DiagnosticsFromContext(ctx)
	if d == nil {
		return ""
	}
	return d.data.ID
}
func (r *Registry) startDiagnostics(ctx context.Context, endpoint string, timing *Timeline) (context.Context, *Diagnostics) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ctx, nil
	}
	id := "req_" + hex.EncodeToString(raw[:])
	now := time.Now()
	d := &Diagnostics{data: RequestDiagnostic{ID: id, Endpoint: endpoint, StartedAt: now, OTLPTraceID: TraceID(ctx), Attempts: []DiagnosticAttempt{}, Events: []DiagnosticEvent{}}, start: now, timing: timing, index: make(map[string]int)}
	d.Event("request", "started", "")
	r.mu.Lock()
	if r.diagnostics == nil {
		r.diagnostics = make(map[string]*Diagnostics)
	}
	r.diagnostics[id] = d
	r.diagnosticOrder = append(r.diagnosticOrder, id)
	if len(r.diagnosticOrder) > MaxDiagnosticRequests {
		delete(r.diagnostics, r.diagnosticOrder[0])
		r.diagnosticOrder = r.diagnosticOrder[1:]
	}
	r.mu.Unlock()
	return context.WithValue(ctx, diagnosticsContextKey{}, d), d
}
func (d *Diagnostics) link(correlation, traceUID string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if id := safeID(correlation); id != "" {
		d.data.CorrelationID = id
	}
	if id := safeID(traceUID); id != "" {
		d.data.AutopilotTraceUID = id
	}
}
func diagnosticEnum(value string, allowed ...string) string {
	for _, v := range allowed {
		if value == v {
			return v
		}
	}
	return "unknown"
}
func (d *Diagnostics) Event(stage, outcome, attemptID string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if stage == "sse_terminal" {
		d.data.TerminalOutcome = diagnosticEnum(outcome, "completed", "failed", "incomplete", "error", "stalled", "repetition", "canceled", "unknown")
	}
	d.eventLocked(stage, outcome, attemptID)
}
func (d *Diagnostics) eventLocked(stage, outcome, attemptID string) {
	if len(d.data.Events) >= MaxDiagnosticEvents {
		d.data.DroppedEvents++
		return
	}
	stage = diagnosticEnum(stage, "request", "selection", "limiter", "build", "attempt", "upstream", "stream", "failover", "sse_terminal")
	outcome = diagnosticEnum(outcome, "started", "pending", "connecting", "first_byte", "streaming", "completed", "failed", "cancelled", "canceled", "racing_lost", "won", "lost", "http_error", "transport_error", "invalid_response", "concurrency", "channel", "scope", "incomplete", "error", "stalled", "repetition", "rejected", "unknown")
	d.data.Events = append(d.data.Events, DiagnosticEvent{Sequence: len(d.data.Events) + 1, ElapsedMs: time.Since(d.start).Milliseconds(), Stage: stage, Outcome: outcome, AttemptID: safeID(attemptID)})
}
func (d *Diagnostics) Attempt(a DiagnosticAttempt) {
	if d == nil || safeID(a.AttemptID) == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	a.ChannelUID = safeID(a.ChannelUID)
	a.KeyUID = safeID(a.KeyUID)
	a.UpstreamRequestID = safeID(a.UpstreamRequestID)
	a.Protocol = diagnosticEnum(strings.ToLower(a.Protocol), "messages", "chat", "responses", "gemini", "images", "vectors", "openai", "claude")
	a.Source = diagnosticEnum(a.Source, "proxy", "capability_test", "healthcheck")
	a.Status = diagnosticEnum(a.Status, "pending", "connecting", "first_byte", "streaming", "completed", "failed", "cancelled", "racing_lost")
	a.ErrorSource = diagnosticEnum(a.ErrorSource, "upstream", "transport", "gateway", "client", "racing")
	if a.ErrorSource == "unknown" {
		a.ErrorSource = ""
	}
	if a.RacingRole != "" {
		a.RacingRole = diagnosticEnum(a.RacingRole, "primary", "shadow")
	}
	if a.RacingStatus != "" {
		a.RacingStatus = diagnosticEnum(a.RacingStatus, "won", "lost")
	}
	if a.StatusCode < 100 || a.StatusCode > 599 {
		a.StatusCode = 0
	}
	if a.DurationMs < 0 {
		a.DurationMs = 0
	}
	idx, ok := d.index[a.AttemptID]
	if !ok {
		if len(d.data.Attempts) >= MaxDiagnosticAttempts {
			d.data.DroppedAttempts++
			return
		}
		idx = len(d.data.Attempts)
		d.index[a.AttemptID] = idx
		d.data.Attempts = append(d.data.Attempts, a)
		d.eventLocked("attempt", a.Status, a.AttemptID)
		return
	}
	previous := d.data.Attempts[idx]
	if a.StartMs == 0 {
		a.StartMs = previous.StartMs
	}
	if previous.Status != a.Status || previous.RacingStatus != a.RacingStatus {
		d.eventLocked("attempt", a.Status, a.AttemptID)
	}
	d.data.Attempts[idx] = a
}
func (d *Diagnostics) Finish(status int, canceled bool) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.data.StatusCode = status
	d.data.Canceled = canceled
	d.data.Completed = true
	outcome := "completed"
	if status >= 400 {
		outcome = "failed"
	}
	if canceled {
		outcome = "canceled"
	}
	d.eventLocked("request", outcome, "")
}
func (d *Diagnostics) Snapshot() RequestDiagnostic {
	d.mu.Lock()
	defer d.mu.Unlock()
	v := d.data
	v.Attempts = append([]DiagnosticAttempt{}, v.Attempts...)
	v.Events = append([]DiagnosticEvent{}, v.Events...)
	v.PhaseTiming = d.timing.Snapshot()
	return v
}
func (r *Registry) LookupDiagnostic(id string) (RequestDiagnostic, bool) {
	if safeID(id) == "" {
		return RequestDiagnostic{}, false
	}
	r.mu.Lock()
	items := make([]*Diagnostics, 0, len(r.diagnostics))
	if d := r.diagnostics[id]; d != nil {
		r.mu.Unlock()
		return d.Snapshot(), true
	}
	for _, d := range r.diagnostics {
		items = append(items, d)
	}
	r.mu.Unlock()
	for _, d := range items {
		d.mu.Lock()
		match := d.data.CorrelationID == id
		d.mu.Unlock()
		if match {
			return d.Snapshot(), true
		}
	}
	return RequestDiagnostic{}, false
}
func (r *Registry) ServeDiagnostic(c fiber.Ctx) error {
	httpbridge.Header(c, "Cache-Control", "no-store")
	id := httpbridge.Param(c, "id")
	if safeID(id) == "" {
		httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "invalid_request_id"})
		return nil
	}
	v, ok := r.LookupDiagnostic(id)
	if !ok {
		httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "request_diagnostic_not_retained"})
		return nil
	}
	httpbridge.JSON(c, 200, fiber.Map{"storage": "memory", "requestCapacity": MaxDiagnosticRequests, "attemptCapacity": MaxDiagnosticAttempts, "eventCapacity": MaxDiagnosticEvents, "request": v})
	return nil
}

func (d *Diagnostics) Elapsed(at time.Time) int64 {
	if d == nil {
		return 0
	}
	v := at.Sub(d.start).Milliseconds()
	if v < 0 {
		return 0
	}
	return v
}

func (d *Diagnostics) DropUpdate() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.data.DroppedEvents++
	d.mu.Unlock()
}
