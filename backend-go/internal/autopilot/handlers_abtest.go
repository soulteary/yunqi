package autopilot

import (
	"log"
	"net/http"
	"os"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// ─── A/B 测试 API 路由注册 ─────────────────────────────────────────────────────────

// ABTestDeps A/B 测试路由的依赖注入。
type ABTestDeps struct {
	Sampler    *ABTestSampler
	Store      *ABTestStore
	CfgManager *config.ConfigManager
}

// RegisterABTestRoutes 注册 A/B 测试 API 路由。
// GET  /api/autopilot/ab-test-results  —— 结果统计 + 最近记录
// POST /api/autopilot/ab-test/emergency-stop —— 紧急停止（等价 Enabled=false）
func RegisterABTestRoutes(group fiber.Router, deps *ABTestDeps) {
	group.Get("/autopilot/ab-test-results", handleABTestResults(deps))
	group.Post("/autopilot/ab-test/emergency-stop", handleABTestEmergencyStop(deps))
}

// ─── 响应类型 ──────────────────────────────────────────────────────────────────────

// ABTestResultsResponse GET /api/autopilot/ab-test-results 响应体。
type ABTestResultsResponse struct {
	Enabled              bool            `json:"enabled"`
	SampleRatio          float64         `json:"sampleRatio"`
	ShadowCandidateCount int             `json:"shadowCandidateCount"`
	BudgetUsed           int             `json:"budgetUsed"`
	BudgetRemaining      int             `json:"budgetRemaining"`
	MaxBudgetPerHour     int             `json:"maxBudgetPerHour"`
	KillSwitchActive     bool            `json:"killSwitchActive"`
	Stats                *ABTestStats    `json:"stats"`
	RecentRecords        []*ABTestRecord `json:"recentRecords"`
	TotalShadowCostUSD   float64         `json:"totalShadowCostUsd"`
}

// ─── 处理函数 ──────────────────────────────────────────────────────────────────────

// handleABTestResults GET /api/autopilot/ab-test-results
// 返回 A/B 测试结果统计、最近记录和预算信息。
func handleABTestResults(deps *ABTestDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		// killSwitch 检查：config 字段 OR 环境变量
		envKillSwitch := false
		if envVal := os.Getenv("AUTOPILOT_KILL_SWITCH"); isTruthyEnv(envVal) {
			envKillSwitch = true
		}
		var configKillSwitch bool
		if deps.CfgManager != nil {
			cfg := deps.CfgManager.GetAutopilotRouting()
			configKillSwitch = cfg.KillSwitch
		}
		killSwitchActive := configKillSwitch || envKillSwitch

		if deps.Sampler == nil || deps.Store == nil {
			httpbridge.JSON(c, http.StatusOK, ABTestResultsResponse{
				Enabled:          false,
				KillSwitchActive: killSwitchActive,
			})
			return nil
		}

		cfg := deps.Sampler.config()
		stats := deps.Store.GetStats()

		// 获取最近 50 条记录
		recentRecords := deps.Store.ListRecent(50)

		resp := ABTestResultsResponse{
			Enabled:              cfg.Enabled,
			SampleRatio:          cfg.SampleRatio,
			ShadowCandidateCount: cfg.ShadowCandidateCount,
			BudgetUsed:           deps.Sampler.BudgetUsed(),
			BudgetRemaining:      deps.Sampler.BudgetRemaining(),
			MaxBudgetPerHour:     cfg.MaxShadowRequestsPerHour,
			KillSwitchActive:     killSwitchActive,
			Stats:                stats,
			RecentRecords:        recentRecords,
			TotalShadowCostUSD:   stats.TotalShadowCostUSD,
		}
		httpbridge.JSON(c, http.StatusOK, resp)
		return nil
	}
}

// ABTestEmergencyStopRequest 紧急停止请求体。
type ABTestEmergencyStopRequest struct {
	Reason string `json:"reason,omitempty"`
}

// handleABTestEmergencyStop POST /api/autopilot/ab-test/emergency-stop
// 紧急停止 A/B 测试（通过 ConfigManager 将 Enabled 设为 false 并持久化）。
func handleABTestEmergencyStop(deps *ABTestDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		// 解析可选 reason
		var req ABTestEmergencyStopRequest
		// 忽略绑定错误，reason 是可选字段
		_ = httpbridge.BindJSON(c, &req)

		reason := req.Reason
		if reason == "" {
			reason = "manual emergency stop via API"
		}

		// 通过 ConfigManager 持久化关闭 ABTest
		if deps.CfgManager != nil {
			if err := deps.CfgManager.SetABTestEnabledAudited(managementAuditContext, false); err != nil {
				log.Printf("[ABTest-EmergencyStop] 持久化关闭失败: %v", err)
				httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{
					"error": "持久化关闭 A/B 测试失败，请手动修改配置文件",
				})
				return nil
			}
		}

		log.Printf("[ABTest-EmergencyStop] 紧急停止已触发并持久化: reason=%s", reason)
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"ok":     true,
			"action": "ab_test_emergency_stop",
			"reason": reason,
			"note":   "A/B 测试已紧急停止，配置已持久化。所有影子请求采样将立即停止。",
		})
		return nil
	}
}

// parsePositiveInt 从查询参数解析正整数，默认值 fallback。
