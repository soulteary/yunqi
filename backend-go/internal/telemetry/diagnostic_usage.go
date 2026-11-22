package telemetry

// DiagnosticUsageSummary contains counters and fixed labels only. One value per
// retained logical request keeps post-cancel accounting visible without a body,
// raw provider usage, arbitrary errors, or an unbounded event stream.
type DiagnosticUsageSummary struct {
	AttemptID                  string `json:"attemptId,omitempty"`
	Protocol                   string `json:"protocol"`
	UsageSource                string `json:"usageSource"`
	Complete                   bool   `json:"complete"`
	Reason                     string `json:"reason,omitempty"`
	InputTokens                int    `json:"inputTokens"`
	OutputTokens               int    `json:"outputTokens"`
	CacheReadInputTokens       int    `json:"cacheReadInputTokens"`
	CacheCreationInputTokens   int    `json:"cacheCreationInputTokens"`
	CacheCreation5mInputTokens int    `json:"cacheCreation5mInputTokens"`
	CacheCreation1hInputTokens int    `json:"cacheCreation1hInputTokens"`
	CacheTTL                   string `json:"cacheTtl,omitempty"`
}

func (d *Diagnostics) SetUsageAfterCancel(summary DiagnosticUsageSummary) {
	if d == nil {
		return
	}
	summary.AttemptID = safeID(summary.AttemptID)
	summary.Protocol = diagnosticEnum(summary.Protocol, "messages", "chat", "responses", "gemini")
	summary.UsageSource = diagnosticEnum(summary.UsageSource, "provider", "mixed", "estimated", "unknown")
	summary.Complete = summary.Complete && summary.UsageSource == "provider"
	if summary.Reason != "" {
		summary.Reason = diagnosticEnum(summary.Reason, "client_cancel_usage_drain_incomplete")
	}
	if summary.CacheTTL != "" {
		summary.CacheTTL = diagnosticEnum(summary.CacheTTL, "5m", "1h", "mixed")
	}
	summary.InputTokens = max(0, summary.InputTokens)
	summary.OutputTokens = max(0, summary.OutputTokens)
	summary.CacheReadInputTokens = max(0, summary.CacheReadInputTokens)
	summary.CacheCreationInputTokens = max(0, summary.CacheCreationInputTokens)
	summary.CacheCreation5mInputTokens = max(0, summary.CacheCreation5mInputTokens)
	summary.CacheCreation1hInputTokens = max(0, summary.CacheCreation1hInputTokens)
	d.mu.Lock()
	d.data.UsageAfterCancel = &summary
	d.mu.Unlock()
}
