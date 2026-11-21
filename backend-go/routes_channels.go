package main

import (
	"net/http"

	"github.com/soulteary/yunqi/internal/handlers"
	"github.com/soulteary/yunqi/internal/handlers/chat"
	"github.com/soulteary/yunqi/internal/handlers/gemini"
	"github.com/soulteary/yunqi/internal/handlers/images"
	"github.com/soulteary/yunqi/internal/handlers/messages"
	"github.com/soulteary/yunqi/internal/handlers/responses"
	"github.com/soulteary/yunqi/internal/handlers/vectors"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/gin-gonic/gin"
)

func (a *application) registerChannelRoutes(apiGroup *gin.RouterGroup) {
	// 渠道保活验证管理 API（六类渠道，挂在各渠道 ping 路由附近）
	registerChannelHealthRoutes := func(channelType string) {
		base := "/" + channelType + "/channels/:id/health"
		if a.healthCheckManager == nil {
			unavailable := func(c *gin.Context) {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "渠道保活验证未启用（指标持久化不可用）"})
			}
			apiGroup.GET(base, unavailable)
			apiGroup.POST(base+"/check", unavailable)
			return
		}
		apiGroup.GET(base, a.healthCheckManager.ChannelHealthHandler(channelType))
		apiGroup.POST(base+"/check", a.healthCheckManager.TriggerChannelCheckHandler(channelType))
	}

	// 上游余额探测管理 API（六类渠道）
	registerChannelBalanceRoutes := func(channelType string) {
		base := "/" + channelType + "/channels/:id/balance"
		if a.balanceProbeManager == nil {
			unavailable := func(c *gin.Context) {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "上游余额探测未启用（指标持久化不可用）"})
			}
			apiGroup.GET(base, unavailable)
			apiGroup.POST(base+"/check", unavailable)
			return
		}
		apiGroup.GET(base, a.balanceProbeManager.ChannelBalanceHandler(channelType))
		apiGroup.POST(base+"/check", a.balanceProbeManager.TriggerChannelBalanceCheckHandler(channelType))
	}

	apiGroup.POST("/responses/channels/:id/copilot/diagnose", responses.DiagnoseCopilotChannel(a.cfgManager))

	// Messages 渠道管理
	apiGroup.GET("/messages/channels", messages.GetUpstreams(a.cfgManager))
	apiGroup.POST("/messages/channels", messages.AddUpstream(a.cfgManager))
	apiGroup.PUT("/messages/channels/:id", messages.UpdateUpstream(a.cfgManager, a.channelScheduler))
	apiGroup.DELETE("/messages/channels/:id", messages.DeleteUpstream(a.cfgManager, a.channelScheduler))
	apiGroup.POST("/messages/channels/:id/keys", messages.AddApiKey(a.cfgManager))
	apiGroup.DELETE("/messages/channels/:id/keys/:apiKey", messages.DeleteApiKey(a.cfgManager))
	apiGroup.POST("/messages/channels/:id/keys/:apiKey/top", messages.MoveApiKeyToTop(a.cfgManager))
	apiGroup.POST("/messages/channels/:id/keys/:apiKey/bottom", messages.MoveApiKeyToBottom(a.cfgManager))
	apiGroup.POST("/messages/channels/:id/keys/restore", handlers.RestoreBlacklistedKey(a.cfgManager, "Messages"))
	apiGroup.POST("/messages/channels/:id/keys/restore-model", handlers.RestoreKeyModel(a.cfgManager, "Messages"))
	apiGroup.POST("/messages/channels/:id/keys/group-model/disable", handlers.DisableGroupModel(a.cfgManager, "Messages"))
	apiGroup.POST("/messages/channels/:id/keys/group-model/restore", handlers.RestoreGroupModel(a.cfgManager, "Messages"))
	apiGroup.POST("/messages/channels/:id/keys/suspend", handlers.SuspendAPIKey(a.cfgManager, "Messages"))
	apiGroup.POST("/messages/channels/:id/keys/resume", handlers.ResumeAPIKey(a.cfgManager, "Messages"))

	// Messages 多渠道调度 API
	apiGroup.POST("/messages/channels/reorder", messages.ReorderChannels(a.cfgManager))
	apiGroup.PATCH("/messages/channels/:id/status", messages.SetChannelStatus(a.cfgManager))
	apiGroup.POST("/messages/channels/:id/resume", handlers.ResumeChannel(a.channelScheduler, a.cfgManager, false))
	apiGroup.POST("/messages/channels/:id/promotion", messages.SetChannelPromotion(a.cfgManager))
	apiGroup.GET("/messages/channels/metrics", handlers.GetChannelMetricsWithConfig(a.messagesMetricsManager, a.cfgManager, false))
	apiGroup.GET("/messages/channels/metrics/history", handlers.GetChannelMetricsHistory(a.messagesMetricsManager, a.cfgManager, false))
	apiGroup.GET("/messages/channels/:id/keys/metrics/history", handlers.GetChannelKeyMetricsHistory(a.messagesMetricsManager, a.cfgManager, false))
	apiGroup.GET("/messages/channels/scheduler/stats", handlers.GetSchedulerStats(a.channelScheduler))
	apiGroup.POST("/messages/channels/scheduler/diagnose", handlers.DiagnoseSchedulerSelection(a.channelScheduler, scheduler.ChannelKindMessages))
	apiGroup.GET("/messages/global/stats/history", handlers.GetGlobalStatsHistory(a.messagesMetricsManager))
	apiGroup.GET("/messages/channels/dashboard", handlers.GetChannelDashboard(a.cfgManager, a.channelScheduler)) // 统一 dashboard 端点，支持 ?type=messages|responses|chat|gemini|images|vectors
	apiGroup.GET("/messages/ping/:id", messages.PingChannel(a.cfgManager))
	apiGroup.GET("/messages/ping", messages.PingAllChannels(a.cfgManager))
	apiGroup.POST("/messages/channels/:id/models", messages.GetChannelModels(a.cfgManager))
	registerChannelHealthRoutes("messages")
	registerChannelBalanceRoutes("messages")
	apiGroup.GET("/messages/models/stats/history", handlers.GetModelStatsHistory(a.messagesMetricsManager))
	apiGroup.GET("/messages/channels/:id/logs", handlers.GetChannelLogs(a.channelScheduler.GetChannelLogStore(scheduler.ChannelKindMessages), a.cfgManager, scheduler.ChannelKindMessages, a.channelScheduler.GetMessagesMetricsManager()))
	apiGroup.GET("/messages/channels/:id/capability-snapshot", handlers.GetCapabilitySnapshot(a.cfgManager, "messages"))
	apiGroup.POST("/messages/channels/:id/capability-test", handlers.TestChannelCapability(a.cfgManager, a.channelScheduler.GetChannelLogStore(scheduler.ChannelKindMessages), "messages"))
	apiGroup.GET("/messages/channels/:id/capability-test/:jobId", handlers.GetCapabilityTestJobStatus(a.cfgManager, "messages"))
	apiGroup.DELETE("/messages/channels/:id/capability-test/:jobId", handlers.CancelCapabilityTestJob(a.cfgManager, "messages"))
	apiGroup.POST("/messages/channels/:id/capability-test/:jobId/retry", handlers.RetryCapabilityTestModel(a.cfgManager, a.channelScheduler.GetChannelLogStore(scheduler.ChannelKindMessages), "messages"))
	apiGroup.POST("/messages/channels/:id/compat-diagnose", handlers.DiagnoseChannelCompat(a.cfgManager, "messages"))

	// Responses 渠道管理
	apiGroup.GET("/responses/channels", responses.GetUpstreams(a.cfgManager))
	apiGroup.POST("/responses/channels", responses.AddUpstream(a.cfgManager))
	apiGroup.PUT("/responses/channels/:id", responses.UpdateUpstream(a.cfgManager, a.channelScheduler))
	apiGroup.DELETE("/responses/channels/:id", responses.DeleteUpstream(a.cfgManager, a.channelScheduler))
	apiGroup.POST("/responses/channels/:id/keys", responses.AddApiKey(a.cfgManager))
	apiGroup.DELETE("/responses/channels/:id/keys/:apiKey", responses.DeleteApiKey(a.cfgManager))
	apiGroup.POST("/responses/channels/:id/keys/:apiKey/top", responses.MoveApiKeyToTop(a.cfgManager))
	apiGroup.POST("/responses/channels/:id/keys/:apiKey/bottom", responses.MoveApiKeyToBottom(a.cfgManager))
	apiGroup.POST("/responses/channels/:id/keys/restore", handlers.RestoreBlacklistedKey(a.cfgManager, "Responses"))
	apiGroup.POST("/responses/channels/:id/keys/restore-model", handlers.RestoreKeyModel(a.cfgManager, "Responses"))
	apiGroup.POST("/responses/channels/:id/keys/group-model/disable", handlers.DisableGroupModel(a.cfgManager, "Responses"))
	apiGroup.POST("/responses/channels/:id/keys/group-model/restore", handlers.RestoreGroupModel(a.cfgManager, "Responses"))
	apiGroup.POST("/responses/channels/:id/keys/suspend", handlers.SuspendAPIKey(a.cfgManager, "Responses"))
	apiGroup.POST("/responses/channels/:id/keys/resume", handlers.ResumeAPIKey(a.cfgManager, "Responses"))

	// Responses 多渠道调度 API
	apiGroup.POST("/responses/channels/reorder", responses.ReorderChannels(a.cfgManager))
	apiGroup.PATCH("/responses/channels/:id/status", responses.SetChannelStatus(a.cfgManager))
	apiGroup.POST("/responses/channels/:id/resume", handlers.ResumeChannel(a.channelScheduler, a.cfgManager, true))
	apiGroup.POST("/responses/channels/:id/promotion", responses.SetChannelPromotion(a.cfgManager))
	apiGroup.GET("/responses/channels/metrics", handlers.GetChannelMetricsWithConfig(a.responsesMetricsManager, a.cfgManager, true))
	apiGroup.GET("/responses/channels/metrics/history", handlers.GetChannelMetricsHistory(a.responsesMetricsManager, a.cfgManager, true))
	apiGroup.GET("/responses/channels/:id/keys/metrics/history", handlers.GetChannelKeyMetricsHistory(a.responsesMetricsManager, a.cfgManager, true))
	apiGroup.POST("/responses/channels/scheduler/diagnose", handlers.DiagnoseSchedulerSelection(a.channelScheduler, scheduler.ChannelKindResponses))
	apiGroup.GET("/responses/global/stats/history", handlers.GetGlobalStatsHistory(a.responsesMetricsManager))
	apiGroup.GET("/responses/ping/:id", responses.PingChannel(a.cfgManager))
	apiGroup.GET("/responses/ping", responses.PingAllChannels(a.cfgManager))
	apiGroup.POST("/responses/channels/:id/models", responses.GetChannelModels(a.cfgManager))
	registerChannelHealthRoutes("responses")
	registerChannelBalanceRoutes("responses")
	apiGroup.GET("/responses/models/stats/history", handlers.GetModelStatsHistory(a.responsesMetricsManager))
	apiGroup.GET("/responses/channels/:id/logs", handlers.GetChannelLogs(a.channelScheduler.GetChannelLogStore(scheduler.ChannelKindResponses), a.cfgManager, scheduler.ChannelKindResponses, a.channelScheduler.GetResponsesMetricsManager()))
	apiGroup.GET("/responses/channels/:id/capability-snapshot", handlers.GetCapabilitySnapshot(a.cfgManager, "responses"))
	apiGroup.POST("/responses/channels/:id/capability-test", handlers.TestChannelCapability(a.cfgManager, a.channelScheduler.GetChannelLogStore(scheduler.ChannelKindResponses), "responses"))
	apiGroup.GET("/responses/channels/:id/capability-test/:jobId", handlers.GetCapabilityTestJobStatus(a.cfgManager, "responses"))
	apiGroup.DELETE("/responses/channels/:id/capability-test/:jobId", handlers.CancelCapabilityTestJob(a.cfgManager, "responses"))
	apiGroup.POST("/responses/channels/:id/capability-test/:jobId/retry", handlers.RetryCapabilityTestModel(a.cfgManager, a.channelScheduler.GetChannelLogStore(scheduler.ChannelKindResponses), "responses"))
	apiGroup.POST("/responses/channels/:id/compat-diagnose", handlers.DiagnoseChannelCompat(a.cfgManager, "responses"))

	// Gemini 渠道管理
	apiGroup.GET("/gemini/channels", gemini.GetUpstreams(a.cfgManager))
	apiGroup.POST("/gemini/channels", gemini.AddUpstream(a.cfgManager))
	apiGroup.PUT("/gemini/channels/:id", gemini.UpdateUpstream(a.cfgManager, a.channelScheduler))
	apiGroup.DELETE("/gemini/channels/:id", gemini.DeleteUpstream(a.cfgManager, a.channelScheduler))
	apiGroup.POST("/gemini/channels/:id/keys", gemini.AddApiKey(a.cfgManager))
	apiGroup.DELETE("/gemini/channels/:id/keys/:apiKey", gemini.DeleteApiKey(a.cfgManager))
	apiGroup.POST("/gemini/channels/:id/keys/:apiKey/top", gemini.MoveApiKeyToTop(a.cfgManager))
	apiGroup.POST("/gemini/channels/:id/keys/:apiKey/bottom", gemini.MoveApiKeyToBottom(a.cfgManager))
	apiGroup.POST("/gemini/channels/:id/keys/restore", handlers.RestoreBlacklistedKey(a.cfgManager, "Gemini"))
	apiGroup.POST("/gemini/channels/:id/keys/restore-model", handlers.RestoreKeyModel(a.cfgManager, "Gemini"))
	apiGroup.POST("/gemini/channels/:id/keys/group-model/disable", handlers.DisableGroupModel(a.cfgManager, "Gemini"))
	apiGroup.POST("/gemini/channels/:id/keys/group-model/restore", handlers.RestoreGroupModel(a.cfgManager, "Gemini"))
	apiGroup.POST("/gemini/channels/:id/keys/suspend", handlers.SuspendAPIKey(a.cfgManager, "Gemini"))
	apiGroup.POST("/gemini/channels/:id/keys/resume", handlers.ResumeAPIKey(a.cfgManager, "Gemini"))

	// Gemini 多渠道调度 API
	apiGroup.POST("/gemini/channels/reorder", gemini.ReorderChannels(a.cfgManager))
	apiGroup.PATCH("/gemini/channels/:id/status", gemini.SetChannelStatus(a.cfgManager))
	apiGroup.POST("/gemini/channels/:id/resume", handlers.ResumeChannelWithKind(a.channelScheduler, a.cfgManager, scheduler.ChannelKindGemini))
	apiGroup.POST("/gemini/channels/:id/promotion", gemini.SetChannelPromotion(a.cfgManager))
	apiGroup.GET("/gemini/channels/metrics", handlers.GetGeminiChannelMetrics(a.geminiMetricsManager, a.cfgManager))
	apiGroup.GET("/gemini/channels/metrics/history", handlers.GetGeminiChannelMetricsHistory(a.geminiMetricsManager, a.cfgManager))
	apiGroup.GET("/gemini/channels/:id/keys/metrics/history", handlers.GetGeminiChannelKeyMetricsHistory(a.geminiMetricsManager, a.cfgManager))
	apiGroup.POST("/gemini/channels/scheduler/diagnose", handlers.DiagnoseSchedulerSelection(a.channelScheduler, scheduler.ChannelKindGemini))
	apiGroup.GET("/gemini/global/stats/history", handlers.GetGlobalStatsHistory(a.geminiMetricsManager))
	apiGroup.GET("/gemini/ping/:id", gemini.PingChannel(a.cfgManager))
	apiGroup.GET("/gemini/ping", gemini.PingAllChannels(a.cfgManager))
	apiGroup.POST("/gemini/channels/:id/models", gemini.GetChannelModels(a.cfgManager))
	registerChannelHealthRoutes("gemini")
	registerChannelBalanceRoutes("gemini")
	apiGroup.GET("/gemini/models/stats/history", handlers.GetModelStatsHistory(a.geminiMetricsManager))
	apiGroup.GET("/gemini/channels/:id/logs", handlers.GetChannelLogs(a.channelScheduler.GetChannelLogStore(scheduler.ChannelKindGemini), a.cfgManager, scheduler.ChannelKindGemini, a.channelScheduler.GetGeminiMetricsManager()))
	apiGroup.GET("/gemini/channels/:id/capability-snapshot", handlers.GetCapabilitySnapshot(a.cfgManager, "gemini"))
	apiGroup.POST("/gemini/channels/:id/capability-test", handlers.TestChannelCapability(a.cfgManager, a.channelScheduler.GetChannelLogStore(scheduler.ChannelKindGemini), "gemini"))
	apiGroup.GET("/gemini/channels/:id/capability-test/:jobId", handlers.GetCapabilityTestJobStatus(a.cfgManager, "gemini"))
	apiGroup.DELETE("/gemini/channels/:id/capability-test/:jobId", handlers.CancelCapabilityTestJob(a.cfgManager, "gemini"))
	apiGroup.POST("/gemini/channels/:id/capability-test/:jobId/retry", handlers.RetryCapabilityTestModel(a.cfgManager, a.channelScheduler.GetChannelLogStore(scheduler.ChannelKindGemini), "gemini"))
	apiGroup.POST("/gemini/channels/:id/compat-diagnose", handlers.DiagnoseChannelCompat(a.cfgManager, "gemini"))

	// Chat 渠道管理
	apiGroup.GET("/chat/channels", chat.GetUpstreams(a.cfgManager))
	apiGroup.POST("/chat/channels", chat.AddUpstream(a.cfgManager))
	apiGroup.PUT("/chat/channels/:id", chat.UpdateUpstream(a.cfgManager, a.channelScheduler))
	apiGroup.DELETE("/chat/channels/:id", chat.DeleteUpstream(a.cfgManager, a.channelScheduler))
	apiGroup.POST("/chat/channels/:id/keys", chat.AddApiKey(a.cfgManager))
	apiGroup.DELETE("/chat/channels/:id/keys/:apiKey", chat.DeleteApiKey(a.cfgManager))
	apiGroup.POST("/chat/channels/:id/keys/:apiKey/top", chat.MoveApiKeyToTop(a.cfgManager))
	apiGroup.POST("/chat/channels/:id/keys/:apiKey/bottom", chat.MoveApiKeyToBottom(a.cfgManager))
	apiGroup.POST("/chat/channels/:id/keys/restore", handlers.RestoreBlacklistedKey(a.cfgManager, "Chat"))
	apiGroup.POST("/chat/channels/:id/keys/restore-model", handlers.RestoreKeyModel(a.cfgManager, "Chat"))
	apiGroup.POST("/chat/channels/:id/keys/group-model/disable", handlers.DisableGroupModel(a.cfgManager, "Chat"))
	apiGroup.POST("/chat/channels/:id/keys/group-model/restore", handlers.RestoreGroupModel(a.cfgManager, "Chat"))
	apiGroup.POST("/chat/channels/:id/keys/suspend", handlers.SuspendAPIKey(a.cfgManager, "Chat"))
	apiGroup.POST("/chat/channels/:id/keys/resume", handlers.ResumeAPIKey(a.cfgManager, "Chat"))

	// Chat 多渠道调度 API
	apiGroup.POST("/chat/channels/reorder", chat.ReorderChannels(a.cfgManager))
	apiGroup.PATCH("/chat/channels/:id/status", chat.SetChannelStatus(a.cfgManager))
	apiGroup.POST("/chat/channels/:id/resume", handlers.ResumeChannelWithKind(a.channelScheduler, a.cfgManager, scheduler.ChannelKindChat))
	apiGroup.POST("/chat/channels/:id/promotion", chat.SetChannelPromotion(a.cfgManager))
	apiGroup.GET("/chat/channels/metrics", handlers.GetChatChannelMetrics(a.chatMetricsManager, a.cfgManager))
	apiGroup.GET("/chat/channels/metrics/history", handlers.GetChatChannelMetricsHistory(a.chatMetricsManager, a.cfgManager))
	apiGroup.GET("/chat/channels/:id/keys/metrics/history", handlers.GetChatChannelKeyMetricsHistory(a.chatMetricsManager, a.cfgManager))
	apiGroup.POST("/chat/channels/scheduler/diagnose", handlers.DiagnoseSchedulerSelection(a.channelScheduler, scheduler.ChannelKindChat))
	apiGroup.GET("/chat/global/stats/history", handlers.GetGlobalStatsHistory(a.chatMetricsManager))
	apiGroup.GET("/chat/ping/:id", chat.PingChannel(a.cfgManager))
	apiGroup.GET("/chat/ping", chat.PingAllChannels(a.cfgManager))
	apiGroup.POST("/chat/channels/:id/models", chat.GetChannelModels(a.cfgManager))
	registerChannelHealthRoutes("chat")
	registerChannelBalanceRoutes("chat")
	apiGroup.GET("/chat/models/stats/history", handlers.GetModelStatsHistory(a.chatMetricsManager))
	apiGroup.GET("/chat/channels/:id/logs", handlers.GetChannelLogs(a.channelScheduler.GetChannelLogStore(scheduler.ChannelKindChat), a.cfgManager, scheduler.ChannelKindChat, a.channelScheduler.GetChatMetricsManager()))
	apiGroup.GET("/chat/channels/:id/capability-snapshot", handlers.GetCapabilitySnapshot(a.cfgManager, "chat"))
	apiGroup.POST("/chat/channels/:id/capability-test", handlers.TestChannelCapability(a.cfgManager, a.channelScheduler.GetChannelLogStore(scheduler.ChannelKindChat), "chat"))
	apiGroup.GET("/chat/channels/:id/capability-test/:jobId", handlers.GetCapabilityTestJobStatus(a.cfgManager, "chat"))
	apiGroup.DELETE("/chat/channels/:id/capability-test/:jobId", handlers.CancelCapabilityTestJob(a.cfgManager, "chat"))
	apiGroup.POST("/chat/channels/:id/capability-test/:jobId/retry", handlers.RetryCapabilityTestModel(a.cfgManager, a.channelScheduler.GetChannelLogStore(scheduler.ChannelKindChat), "chat"))
	apiGroup.POST("/chat/channels/:id/compat-diagnose", handlers.DiagnoseChannelCompat(a.cfgManager, "chat"))
	apiGroup.GET("/chat/channels/scheduler/stats", handlers.GetSchedulerStats(a.channelScheduler))

	// Images 渠道管理
	apiGroup.GET("/images/channels", images.GetUpstreams(a.cfgManager))
	apiGroup.POST("/images/channels", images.AddUpstream(a.cfgManager))
	apiGroup.PUT("/images/channels/:id", images.UpdateUpstream(a.cfgManager, a.channelScheduler))
	apiGroup.DELETE("/images/channels/:id", images.DeleteUpstream(a.cfgManager, a.channelScheduler))
	apiGroup.POST("/images/channels/:id/keys", images.AddApiKey(a.cfgManager))
	apiGroup.DELETE("/images/channels/:id/keys/:apiKey", images.DeleteApiKey(a.cfgManager))
	apiGroup.POST("/images/channels/:id/keys/:apiKey/top", images.MoveApiKeyToTop(a.cfgManager))
	apiGroup.POST("/images/channels/:id/keys/:apiKey/bottom", images.MoveApiKeyToBottom(a.cfgManager))
	apiGroup.POST("/images/channels/:id/keys/restore", handlers.RestoreBlacklistedKey(a.cfgManager, "Images"))
	apiGroup.POST("/images/channels/:id/keys/restore-model", handlers.RestoreKeyModel(a.cfgManager, "Images"))
	apiGroup.POST("/images/channels/:id/keys/group-model/disable", handlers.DisableGroupModel(a.cfgManager, "Images"))
	apiGroup.POST("/images/channels/:id/keys/group-model/restore", handlers.RestoreGroupModel(a.cfgManager, "Images"))
	apiGroup.POST("/images/channels/:id/keys/suspend", handlers.SuspendAPIKey(a.cfgManager, "Images"))
	apiGroup.POST("/images/channels/:id/keys/resume", handlers.ResumeAPIKey(a.cfgManager, "Images"))

	// Images 多渠道调度 API
	apiGroup.POST("/images/channels/reorder", images.ReorderChannels(a.cfgManager))
	apiGroup.PATCH("/images/channels/:id/status", images.SetChannelStatus(a.cfgManager))
	apiGroup.POST("/images/channels/:id/resume", handlers.ResumeChannelWithKind(a.channelScheduler, a.cfgManager, scheduler.ChannelKindImages))
	apiGroup.POST("/images/channels/:id/promotion", images.SetChannelPromotion(a.cfgManager))
	apiGroup.GET("/images/channels/metrics", handlers.GetImagesChannelMetrics(a.imagesMetricsManager, a.cfgManager))
	apiGroup.GET("/images/channels/metrics/history", handlers.GetImagesChannelMetricsHistory(a.imagesMetricsManager, a.cfgManager))
	apiGroup.GET("/images/channels/:id/keys/metrics/history", handlers.GetImagesChannelKeyMetricsHistory(a.imagesMetricsManager, a.cfgManager))
	apiGroup.POST("/images/channels/scheduler/diagnose", handlers.DiagnoseSchedulerSelection(a.channelScheduler, scheduler.ChannelKindImages))
	apiGroup.GET("/images/global/stats/history", handlers.GetGlobalStatsHistory(a.imagesMetricsManager))
	apiGroup.GET("/images/ping/:id", images.PingChannel(a.cfgManager))
	apiGroup.GET("/images/ping", images.PingAllChannels(a.cfgManager))
	apiGroup.POST("/images/channels/:id/models", images.GetChannelModels(a.cfgManager))
	registerChannelHealthRoutes("images")
	registerChannelBalanceRoutes("images")
	apiGroup.GET("/images/models/stats/history", handlers.GetModelStatsHistory(a.imagesMetricsManager))
	apiGroup.GET("/images/channels/:id/logs", handlers.GetChannelLogs(a.channelScheduler.GetChannelLogStore(scheduler.ChannelKindImages), a.cfgManager, scheduler.ChannelKindImages, a.channelScheduler.GetImagesMetricsManager()))

	// Vectors 渠道管理
	apiGroup.GET("/vectors/channels", vectors.GetUpstreams(a.cfgManager))
	apiGroup.POST("/vectors/channels", vectors.AddUpstream(a.cfgManager))
	apiGroup.PUT("/vectors/channels/:id", vectors.UpdateUpstream(a.cfgManager, a.channelScheduler))
	apiGroup.DELETE("/vectors/channels/:id", vectors.DeleteUpstream(a.cfgManager, a.channelScheduler))
	apiGroup.POST("/vectors/channels/:id/keys", vectors.AddApiKey(a.cfgManager))
	apiGroup.DELETE("/vectors/channels/:id/keys/:apiKey", vectors.DeleteApiKey(a.cfgManager))
	apiGroup.POST("/vectors/channels/:id/keys/:apiKey/top", vectors.MoveApiKeyToTop(a.cfgManager))
	apiGroup.POST("/vectors/channels/:id/keys/:apiKey/bottom", vectors.MoveApiKeyToBottom(a.cfgManager))
	apiGroup.POST("/vectors/channels/:id/keys/restore", handlers.RestoreBlacklistedKey(a.cfgManager, "Vectors"))
	apiGroup.POST("/vectors/channels/:id/keys/restore-model", handlers.RestoreKeyModel(a.cfgManager, "Vectors"))
	apiGroup.POST("/vectors/channels/:id/keys/group-model/disable", handlers.DisableGroupModel(a.cfgManager, "Vectors"))
	apiGroup.POST("/vectors/channels/:id/keys/group-model/restore", handlers.RestoreGroupModel(a.cfgManager, "Vectors"))
	apiGroup.POST("/vectors/channels/:id/keys/suspend", handlers.SuspendAPIKey(a.cfgManager, "Vectors"))
	apiGroup.POST("/vectors/channels/:id/keys/resume", handlers.ResumeAPIKey(a.cfgManager, "Vectors"))

	// Vectors 多渠道调度 API
	apiGroup.POST("/vectors/channels/reorder", vectors.ReorderChannels(a.cfgManager))
	apiGroup.PATCH("/vectors/channels/:id/status", vectors.SetChannelStatus(a.cfgManager))
	apiGroup.POST("/vectors/channels/:id/resume", handlers.ResumeChannelWithKind(a.channelScheduler, a.cfgManager, scheduler.ChannelKindVectors))
	apiGroup.POST("/vectors/channels/:id/promotion", vectors.SetChannelPromotion(a.cfgManager))
	apiGroup.GET("/vectors/channels/metrics", handlers.GetVectorsChannelMetrics(a.vectorsMetricsManager, a.cfgManager))
	apiGroup.GET("/vectors/channels/metrics/history", handlers.GetVectorsChannelMetricsHistory(a.vectorsMetricsManager, a.cfgManager))
	apiGroup.GET("/vectors/channels/:id/keys/metrics/history", handlers.GetVectorsChannelKeyMetricsHistory(a.vectorsMetricsManager, a.cfgManager))
	apiGroup.POST("/vectors/channels/scheduler/diagnose", handlers.DiagnoseSchedulerSelection(a.channelScheduler, scheduler.ChannelKindVectors))
	apiGroup.GET("/vectors/global/stats/history", handlers.GetGlobalStatsHistory(a.vectorsMetricsManager))
	apiGroup.GET("/vectors/ping/:id", vectors.PingChannel(a.cfgManager))
	apiGroup.GET("/vectors/ping", vectors.PingAllChannels(a.cfgManager))
	apiGroup.POST("/vectors/channels/:id/models", vectors.GetChannelModels(a.cfgManager))
	registerChannelHealthRoutes("vectors")
	registerChannelBalanceRoutes("vectors")
	apiGroup.GET("/vectors/models/stats/history", handlers.GetModelStatsHistory(a.vectorsMetricsManager))
	apiGroup.GET("/vectors/channels/:id/logs", handlers.GetChannelLogs(a.channelScheduler.GetChannelLogStore(scheduler.ChannelKindVectors), a.cfgManager, scheduler.ChannelKindVectors, a.channelScheduler.GetVectorsMetricsManager()))

}
