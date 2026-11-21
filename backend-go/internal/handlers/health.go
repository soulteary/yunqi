package handlers

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// HealthCheck 健康检查处理器
func HealthCheck(envCfg *config.EnvConfig, cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		config := cfgManager.GetConfig()

		healthData := fiber.Map{
			"status":    "healthy",
			"timestamp": time.Now().Format(time.RFC3339),
			"uptime":    time.Since(startTime).Seconds(),
			"mode":      envCfg.Env,
			"version":   getVersion(),
			"config": fiber.Map{
				"upstreamCount": len(config.Upstream),
			},
		}
		httpbridge.JSON(c, 200, healthData)
		return nil
	}
}

// getVersion 获取版本信息
func getVersion() fiber.Map {
	// 这些变量在编译时通过 -ldflags 注入
	// 从根目录 VERSION 文件读取
	return fiber.Map{
		"version":   getVersionString(),
		"buildTime": getBuildTime(),
		"gitCommit": getGitCommit(),
	}
}

// 以下函数用于从 main 包获取版本信息
// 由于无法直接导入 main 包，使用默认值
var (
	versionString = "v0.0.0-dev"
	buildTime     = "unknown"
	gitCommit     = "unknown"
)

func getVersionString() string { return versionString }
func getBuildTime() string     { return buildTime }
func getGitCommit() string     { return gitCommit }

// SetVersionInfo 设置版本信息（从 main 调用）
func SetVersionInfo(version, build, commit string) {
	versionString = version
	buildTime = build
	gitCommit = commit
}

// SaveConfigHandler 配置保存处理器
func SaveConfigHandler(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := cfgManager.SaveConfig(); err != nil {
			httpbridge.JSON(c, 500, fiber.Map{
				"status":    "error",
				"message":   "配置保存失败",
				"error":     err.Error(),
				"timestamp": time.Now().Format(time.RFC3339),
			})
			return nil
		}

		config := cfgManager.GetConfig()
		httpbridge.JSON(c, 200, fiber.Map{
			"status":    "success",
			"message":   "配置已保存",
			"timestamp": time.Now().Format(time.RFC3339),
			"config": fiber.Map{
				"upstreamCount": len(config.Upstream),
			},
		})
		return nil
	}
}

// DevInfo 开发信息处理器
func DevInfo(envCfg *config.EnvConfig, cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		httpbridge.JSON(c, 200, fiber.Map{
			"status":      "development",
			"timestamp":   time.Now().Format(time.RFC3339),
			"config":      cfgManager.GetConfig(),
			"environment": envCfg,
		})
		return nil
	}
}

var startTime = time.Now()
