package healthcheck

import (
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
)

// PolicyView 解析后保活策略的管理 API 视图（Duration 转分钟/毫秒便于前端展示）
type PolicyView struct {
	Enabled         bool    `json:"enabled"`
	IntervalMinutes float64 `json:"intervalMinutes"`
	VerifyRealCall  bool    `json:"verifyRealCall"`
	VerifyModel     string  `json:"verifyModel,omitempty"`
	MaxConcurrency  int     `json:"maxConcurrency"`
	TimeoutMs       int64   `json:"timeoutMs"`
}

// ChannelHealthView 渠道保活验证状态（策略 + key 验证记录）
type ChannelHealthView struct {
	ChannelType string                    `json:"channelType"`
	ChannelID   string                    `json:"channelId"`
	Policy      PolicyView                `json:"policy"`
	Records     []metrics.KeyHealthRecord `json:"records"`
}

func toPolicyView(p config.ResolvedHealthCheckPolicy) PolicyView {
	return PolicyView{
		Enabled:         p.Enabled,
		IntervalMinutes: p.Interval.Minutes(),
		VerifyRealCall:  p.VerifyRealCall,
		VerifyModel:     p.VerifyModel,
		MaxConcurrency:  p.MaxConcurrency,
		TimeoutMs:       p.Timeout.Milliseconds(),
	}
}

// ChannelHealth 返回指定渠道的解析策略与 key 健康记录；渠道不存在时返回 nil。
func (m *Manager) ChannelHealth(channelType string, channelIndex int) *ChannelHealthView {
	cfg := m.getConfig()
	upstreams := UpstreamsFor(&cfg, channelType)
	if channelIndex < 0 || channelIndex >= len(upstreams) {
		return nil
	}
	upstream := &upstreams[channelIndex]
	policy := cfg.ResolveHealthCheckPolicy(upstream)
	channelID := stableChannelID(upstream, channelIndex)
	records, err := m.store.GetKeyHealthForChannel(channelType, channelID)
	if err != nil {
		records = nil
	}
	if records == nil {
		records = []metrics.KeyHealthRecord{}
	}
	return &ChannelHealthView{
		ChannelType: channelType,
		ChannelID:   channelID,
		Policy:      toPolicyView(policy),
		Records:     records,
	}
}

// ChannelHealthHandler GET /api/{type}/channels/:id/health
func (m *Manager) ChannelHealthHandler(channelType string) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := strconv.Atoi(httpbridge.Param(c, "id"))
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid channel ID"})
			return nil
		}
		view := m.ChannelHealth(channelType, id)
		if view == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "Channel not found"})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, view)
		return nil
	}
}

// TriggerChannelCheckHandler POST /api/{type}/channels/:id/health/check
// 异步触发该渠道立即验证（202 返回，结果通过 GET health 查询）
func (m *Manager) TriggerChannelCheckHandler(channelType string) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := strconv.Atoi(httpbridge.Param(c, "id"))
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid channel ID"})
			return nil
		}
		cfg := m.getConfig()
		upstreams := UpstreamsFor(&cfg, channelType)
		if id < 0 || id >= len(upstreams) {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "Channel not found"})
			return nil
		}
		accepted := m.TriggerChannelCheck(channelType, id)
		httpbridge.JSON(c, http.StatusAccepted, fiber.Map{
			"message": "保活验证已触发，请稍后通过 GET 查询结果",
			"queued":  accepted,
		})
		return nil
	}
}
