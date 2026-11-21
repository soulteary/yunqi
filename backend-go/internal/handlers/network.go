package handlers

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/httpclient"
	"github.com/soulteary/yunqi/internal/utils"
)

func NetworkInterfaces() fiber.Handler {
	return func(c fiber.Ctx) error {
		interfaces, err := httpclient.ListNetworkInterfaces()
		if err != nil {
			httpbridge.JSON(c, 500, fiber.Map{"error": "无法枚举网卡"})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{"interfaces": interfaces})
		return nil
	}
}
func DiagnoseNetwork(cm *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req struct {
			ChannelUID        string                      `json:"channelUid"`
			BaseURL           string                      `json:"baseUrl"`
			ProxyURL          string                      `json:"proxyUrl"`
			ProxyPreferDirect bool                        `json:"proxyPreferDirect"`
			OutboundNetwork   config.OutboundNetworkPatch `json:"outboundNetwork"`
			Insecure          bool                        `json:"insecureSkipVerify"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "无效的请求体"})
			return nil
		}
		opts := httpclient.ClientOptions{ProxyURL: req.ProxyURL, ProxyPreferDirect: req.ProxyPreferDirect, OutboundNetwork: req.OutboundNetwork.Value, Insecure: req.Insecure}
		if req.ChannelUID != "" {
			lc := cm.GetLogicalChannel(req.ChannelUID)
			if lc == nil || len(lc.BaseURLs) == 0 {
				httpbridge.JSON(c, 404, fiber.Map{"error": "渠道不存在"})
				return nil
			}
			req.BaseURL = lc.BaseURLs[0]
			if lc.Settings != nil {
				opts.ProxyURL = lc.Settings.ProxyURL
				opts.ProxyPreferDirect = lc.Settings.ProxyPreferDirect
				opts.OutboundNetwork = lc.Settings.OutboundNetwork
				opts.Insecure = lc.Settings.InsecureSkipVerify
			}
		}
		if err := utils.ValidateBaseURL(req.BaseURL); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "无效的 baseUrl"})
			return nil
		}
		if err := config.ValidateOutboundNetwork(opts.OutboundNetwork, opts.ProxyURL); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"results": httpclient.DiagnoseNetwork(httpbridge.Request(c).Context(), req.BaseURL, opts)})
		return nil
	}
}
