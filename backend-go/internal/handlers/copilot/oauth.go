package copilot

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	corecopilot "github.com/soulteary/yunqi/internal/copilot"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/httpclient"
)

var oauthRequestTimeout = 30 * time.Second
var newOAuthClient = corecopilot.NewOAuthClient

type tokenRequest struct {
	DeviceCode string `json:"deviceCode"`
	ProxyURL   string `json:"proxyUrl"`
}

type verifyRequest struct {
	AccessToken string `json:"accessToken"`
	ProxyURL    string `json:"proxyUrl"`
}

type deviceCodeRequest struct {
	ProxyURL string `json:"proxyUrl"`
}

func oauthErrorMessage(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded") {
		return "GitHub OAuth 请求超时：无法连接 github.com，请检查网络或代理后重试"
	}
	return err.Error()
}

func oauthHTTPClient(proxyURL string) *http.Client {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return nil
	}
	return httpclient.GetManager().NewStandardClient(oauthRequestTimeout, false, proxyURL)
}

func bindOptionalOAuthRequest(c fiber.Ctx, out interface{}) bool {
	if httpbridge.Request(c).Body == nil || httpbridge.Request(c).ContentLength == 0 {
		return true
	}
	if err := httpbridge.BindJSON(c, out); err != nil {
		httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "invalid request body"})
		return false
	}
	return true
}

// RequestDeviceCode 发起 GitHub Copilot OAuth Device Flow。
func RequestDeviceCode() fiber.Handler {
	return func(c fiber.Ctx) error {
		var req deviceCodeRequest
		if !bindOptionalOAuthRequest(c, &req) {
			return nil
		}

		ctx, cancel := context.WithTimeout(httpbridge.Request(c).Context(), oauthRequestTimeout)
		defer cancel()

		log.Printf("[Copilot-OAuth] 请求 GitHub device code: ip=%s timeout=%s", httpbridge.ClientIP(c), oauthRequestTimeout)
		client := newOAuthClient(oauthHTTPClient(req.ProxyURL))
		deviceCode, err := client.RequestDeviceCode(ctx)
		if err != nil {
			log.Printf("[Copilot-OAuth] GitHub device code 请求失败: ip=%s error=%v", httpbridge.ClientIP(c), err)
			httpbridge.JSON(c, http.StatusBadGateway, fiber.Map{"error": oauthErrorMessage(err)})
			return nil
		}

		log.Printf("[Copilot-OAuth] GitHub device code 请求成功: ip=%s expiresIn=%d interval=%d", httpbridge.ClientIP(c), deviceCode.ExpiresIn, deviceCode.Interval)
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"deviceCode":      deviceCode.DeviceCode,
			"userCode":        deviceCode.UserCode,
			"verificationUri": deviceCode.VerificationURI,
			"expiresIn":       deviceCode.ExpiresIn,
			"interval":        deviceCode.Interval,
		})
		return nil
	}
}

// PollAccessToken 轮询 GitHub OAuth access token。
func PollAccessToken() fiber.Handler {
	return func(c fiber.Ctx) error {
		var req tokenRequest
		if err := httpbridge.BindJSON(c, &req); err != nil || strings.TrimSpace(req.DeviceCode) == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "deviceCode is required"})
			return nil
		}

		ctx, cancel := context.WithTimeout(httpbridge.Request(c).Context(), oauthRequestTimeout)
		defer cancel()

		client := newOAuthClient(oauthHTTPClient(req.ProxyURL))
		token, err := client.PollAccessToken(ctx, req.DeviceCode)
		if err != nil {
			log.Printf("[Copilot-OAuth] GitHub access token 轮询失败: ip=%s error=%v", httpbridge.ClientIP(c), err)
			httpbridge.JSON(c, http.StatusBadGateway, fiber.Map{"error": oauthErrorMessage(err)})
			return nil
		}
		if token.Error != "" {
			httpbridge.JSON(c, http.StatusOK, fiber.Map{
				"error":            token.Error,
				"errorDescription": token.ErrorDescription,
			})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"accessToken": token.AccessToken,
			"tokenType":   token.TokenType,
			"scope":       token.Scope,
		})
		return nil
	}
}

// VerifyToken 验证 GitHub OAuth token 并返回用户信息。
func VerifyToken() fiber.Handler {
	return func(c fiber.Ctx) error {
		var req verifyRequest
		if err := httpbridge.BindJSON(c, &req); err != nil || strings.TrimSpace(req.AccessToken) == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "accessToken is required"})
			return nil
		}

		ctx, cancel := context.WithTimeout(httpbridge.Request(c).Context(), oauthRequestTimeout)
		defer cancel()

		client := newOAuthClient(oauthHTTPClient(req.ProxyURL))
		user, err := client.VerifyUser(ctx, req.AccessToken)
		if err != nil {
			log.Printf("[Copilot-OAuth] GitHub token 验证失败: ip=%s error=%v", httpbridge.ClientIP(c), err)
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": oauthErrorMessage(err)})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"login":     user.Login,
			"id":        user.ID,
			"avatarUrl": user.AvatarURL,
			"htmlUrl":   user.HTMLURL,
		})
		return nil
	}
}
