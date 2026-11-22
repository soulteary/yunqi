package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func (a *application) serve(r *fiber.App) {
	// 启动服务器
	addr := listenAddressForEnv(a.envCfg)
	endpoint := endpointForEnv(a.envCfg)

	serviceContext, cancelService := context.WithCancel(context.Background())
	defer cancelService()
	a.envCfg.ServiceContext = serviceContext
	drain := newRequestDrain()
	// 创建 HTTP 服务器
	srv := &http.Server{
		Addr:              addr,
		Handler:           drain.wrap(httpbridge.Handler(r)),
		ConnState:         drain.connState,
		ConnContext:       drain.connContext,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       time.Duration(a.envCfg.ServerReadTimeout) * time.Millisecond, // 仅控制服务端读取入站请求，避免与上游请求超时耦合
		IdleTimeout:       120 * time.Second,
	}
	if err := configureServerTLS(srv, a.envCfg); err != nil {
		log.Fatalf("[Server-Fatal] HTTPS 配置无效: %v", err)
	}

	fmt.Printf("\n[Server-Startup] 云起 Yunqi API代理服务器已启动\n")
	fmt.Printf("[Server-Info] 版本: %s\n", Version)
	if BuildTime != "unknown" {
		fmt.Printf("[Server-Info] 构建时间: %s\n", BuildTime)
	}
	if GitCommit != "unknown" {
		fmt.Printf("[Server-Info] Git提交: %s\n", GitCommit)
	}
	fmt.Printf("\n")
	fmt.Printf("[Server-Info] 协议: %s\n", strings.ToUpper(endpoint.Scheme))
	fmt.Printf("[Server-Info] 监听地址: %s\n", addr)
	fmt.Printf("[Server-Info] 管理界面: %s\n", endpoint.URL(""))
	fmt.Printf("[Server-Info] API 地址: %s\n", endpoint.URL("/v1"))
	if a.envCfg.EnableHTTPS {
		if a.envCfg.TLSCertFile == "" {
			fmt.Printf("[Server-Info] HTTPS 证书: 自动生成 localhost 自签名证书（仅建议本地使用）\n")
		} else {
			fmt.Printf("[Server-Info] HTTPS 证书: %s\n", a.envCfg.TLSCertFile)
		}
		fmt.Printf("[Server-Info] HTTP 兼容: 已启用（同端口同时接受 HTTP/HTTPS）\n")
	}
	fmt.Printf("\n")
	fmt.Printf("[Server-Info] Claude Messages: POST /v1/messages\n")
	fmt.Printf("[Server-Info] Codex Responses: POST /v1/responses\n")
	fmt.Printf("[Server-Info] Gemini API: POST /v1beta/models/{model}:generateContent\n")
	fmt.Printf("[Server-Info] Gemini API: POST /v1beta/models/{model}:streamGenerateContent\n")
	fmt.Printf("[Server-Info] Chat Completions: POST /v1/chat/completions\n")
	fmt.Printf("[Server-Info] Images Generations: POST /v1/images/generations\n")
	fmt.Printf("[Server-Info] Images Edits: POST /v1/images/edits\n")
	fmt.Printf("[Server-Info] Images Variations: POST /v1/images/variations\n")
	fmt.Printf("[Server-Info] Embeddings: POST /v1/embeddings\n")
	fmt.Printf("[Server-Info] 健康检查: GET /health\n")
	fmt.Printf("\n")
	fmt.Printf("[Server-Info] 环境: %s\n", a.envCfg.Env)
	fmt.Printf("[Server-Info] 配置文件: %s\n", a.paths.ConfigPath)
	if a.paths.LogDir == "none" {
		fmt.Printf("[Server-Info] 日志文件输出: 已禁用（仅控制台）\n")
	} else {
		fmt.Printf("[Server-Info] 日志目录: %s\n", a.paths.LogDir)
	}
	// 生产环境检查：必须设置有效的访问密钥
	if a.envCfg.IsProduction() && a.envCfg.ProxyAccessKey == "your-proxy-access-key" {
		log.Fatal("[Server-Fatal] 生产环境必须设置 PROXY_ACCESS_KEY，禁止使用默认值")
	}
	if err := a.envCfg.ValidateAccessKeys(); err != nil {
		log.Fatalf("[Server-Fatal] 访问密钥配置无效: %v", err)
	}
	// 打印访问控制密钥的脱密内容和设置情况，避免用户混淆
	fmt.Printf("[Server-Info] 代理访问密钥 (PROXY_ACCESS_KEY): %s\n", maskKey(a.envCfg.ProxyAccessKey))
	if a.envCfg.HasExtraProxyAccessKeys() {
		fmt.Printf("[Server-Info] 额外代理访问密钥 (EXTRA_PROXY_ACCESS_KEYS): %d 个已启用\n", len(a.envCfg.ExtraProxyAccessKeys))
	}
	if a.envCfg.AdminAccessKey != "" {
		fmt.Printf("[Server-Info] 管理 API 密钥 (ADMIN_ACCESS_KEY): %s (已启用独立管理密钥)\n", maskKey(a.envCfg.AdminAccessKey))
	} else {
		fmt.Printf("[Server-Info] 管理 API 密钥 (ADMIN_ACCESS_KEY): 未设置 (回退到 PROXY_ACCESS_KEY)\n")
	}
	fmt.Printf("\n")

	// 用于传递关闭结果
	shutdownDone := make(chan struct{})

	// 优雅关闭：监听系统信号
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		signal.Stop(sigChan) // 停止信号监听，避免资源泄漏

		log.Println("[Server-Shutdown] 收到关闭信号，正在优雅关闭服务器...")

		cancelService()
		drain.begin()
		// 创建超时上下文
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("[Server-Shutdown] 警告: 服务器关闭时发生错误: %v", err)
		} else {
			log.Println("[Server-Shutdown] 服务器已安全关闭")
		}

		// Shutdown excludes hijacked connections. Wait for all tracked handlers before
		// closing queues/databases; after the grace period cancel remaining streams.
		if err := drain.wait(ctx); err != nil {
			drain.cancelRemaining()
			_ = srv.Close()
			finalCtx, finalCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer finalCancel()
			if err := drain.wait(finalCtx); err != nil {
				log.Printf("[Server-Shutdown] 未排空请求，跳过关闭仍可能写入的存储: %v", err)
				close(shutdownDone)
				return
			}
		}
		a.stopBackgroundTasks()
		close(shutdownDone)
	}()

	// 启动服务器（阻塞直到关闭）
	if err := startHTTPServer(srv, a.envCfg); err != nil && err != http.ErrServerClosed {
		log.Fatalf("服务器启动失败: %v", err)
	}

	// 等待关闭完成（带超时保护，避免死锁）
	select {
	case <-shutdownDone:
		// 正常关闭完成
	case <-time.After(30 * time.Second):
		log.Println("[Server-Shutdown] 警告: 等待关闭超时")
	}
}

// maskKey 对密钥进行脱密处理，保留首尾部分字符，中间用 * 遮蔽
func maskKey(key string) string {
	if key == "" {
		return "未设置"
	}
	if key == "your-proxy-access-key" {
		return "your-proxy-access-key (默认值，不安全)"
	}
	n := len(key)
	var masked string
	if n <= 3 {
		masked = key[:1] + "****"
	} else if n <= 4 {
		masked = key[:1] + "****" + key[n-1:]
	} else if n <= 8 {
		masked = key[:1] + "****" + key[n-1:]
	} else {
		masked = key[:2] + "****" + key[n-2:]
	}
	return masked + " (已脱敏，不可直接复制)"
}
