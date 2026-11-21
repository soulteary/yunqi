package middleware

import (
	"strings"

	"compress/gzip"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// GzipMiddleware 为静态资源启用 Gzip 压缩
// 使用白名单模式：仅压缩已知的静态资源路径，避免意外压缩流式响应
func GzipMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		if !isCompressibleResource(c.Path()) || !strings.Contains(httpbridge.GetHeader(c, "Accept-Encoding"), "gzip") {
			return c.Next()
		}
		original := httpbridge.Writer(c)
		original.Header().Set("Content-Encoding", "gzip")
		original.Header().Add("Vary", "Accept-Encoding")
		original.Header().Del("Content-Length")
		compressor := gzip.NewWriter(original)
		httpbridge.SetWriter(c, &gzipResponseWriter{ResponseWriter: original, compressor: compressor})
		defer func() { _ = compressor.Close(); httpbridge.SetWriter(c, original) }()
		return c.Next()
	}
}

type gzipResponseWriter struct {
	httpbridge.ResponseWriter
	compressor *gzip.Writer
}

func (w *gzipResponseWriter) Write(p []byte) (int, error) {
	w.Header().Del("Content-Length")
	return w.compressor.Write(p)
}
func (w *gzipResponseWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *gzipResponseWriter) Flush()                            { _ = w.compressor.Flush(); w.ResponseWriter.Flush() }

// isCompressibleResource 判断是否为可压缩的静态资源路径
func isCompressibleResource(path string) bool {
	// 1. Vite 构建产物目录
	if strings.HasPrefix(path, "/assets/") {
		return true
	}

	// 2. 根路径静态文件（HTML、favicon 等）
	if path == "/" || path == "/index.html" || path == "/favicon.ico" {
		return true
	}

	// 3. 按扩展名匹配（兜底）
	staticExts := []string{".js", ".css", ".svg", ".woff", ".woff2", ".ttf", ".eot"}
	for _, ext := range staticExts {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}

	return false
}
