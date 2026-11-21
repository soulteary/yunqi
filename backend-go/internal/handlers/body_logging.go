package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/logger"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/redaction"
)

func bodyLoggingResponse(state config.BodyLoggingState, env *config.EnvConfig) fiber.Map {
	return fiber.Map{"configured": state.Configured, "runtime": state.Runtime, "revision": state.Revision, "audit": state.Audit,
		"effective":                  fiber.Map{"requestBodies": state.Runtime.RequestBodies && env.EnableRequestLogs && env.IsDevelopment(), "responseBodies": state.Runtime.ResponseBodies && env.EnableResponseLogs && env.IsDevelopment()},
		"requiresDevelopmentLogging": true, "retentionDays": env.LogMaxAge, "maxFileSizeMB": env.LogMaxSize, "auditEntryLimit": config.MaxBodyLogAuditEntries}
}
func GetBodyLogging(cm *config.ConfigManager, env *config.EnvConfig) fiber.Handler {
	return func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "logs.body.configure", "") {
			return nil
		}
		httpbridge.JSON(c, 200, bodyLoggingResponse(cm.GetBodyLoggingState(), env))
		return nil
	}
}
func UpdateBodyLogging(cm *config.ConfigManager, env *config.EnvConfig) fiber.Handler {
	return func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "logs.body.configure", "") {
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(httpbridge.Request(c).Body, 4097))
		if err != nil || len(data) > 4096 {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid body logging settings"})
			return nil
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil || len(raw) == 0 {
			httpbridge.JSON(c, 400, fiber.Map{"error": "at least one body logging field is required"})
			return nil
		}
		for key, value := range raw {
			if key != "requestBodies" && key != "responseBodies" && key != "maxBytes" {
				httpbridge.JSON(c, 400, fiber.Map{"error": "unknown body logging field: " + key})
				return nil
			}
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				httpbridge.JSON(c, 400, fiber.Map{"error": key + " requires a concrete value; omit to retain"})
				return nil
			}
		}
		var patch struct {
			RequestBodies  *bool `json:"requestBodies"`
			ResponseBodies *bool `json:"responseBodies"`
			MaxBytes       *int  `json:"maxBytes"`
		}
		if err := json.Unmarshal(data, &patch); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid body logging field type"})
			return nil
		}
		before := cm.GetBodyLoggingState()
		settings := before.Configured
		if patch.RequestBodies != nil {
			settings.RequestBodies = *patch.RequestBodies
		}
		if patch.ResponseBodies != nil {
			settings.ResponseBodies = *patch.ResponseBodies
		}
		if patch.MaxBytes != nil {
			settings.MaxBytes = *patch.MaxBytes
		}
		revision := httpbridge.GetHeader(c, "If-Match")
		if revision == "" {
			revision = before.Revision
		}
		state, err := cm.UpdateBodyLogging(middleware.GetAdminActorID(c), revision, settings)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, config.ErrBodyLogRevisionConflict) {
				status = http.StatusConflict
			} else if settings.MaxBytes != 0 && (settings.MaxBytes < 256 || settings.MaxBytes > 65536) {
				status = 400
			}
			httpbridge.JSON(c, status, fiber.Map{"error": err.Error(), "persisted": false, "state": bodyLoggingResponse(state, env)})
			return nil
		}
		result := bodyLoggingResponse(state, env)
		result["persisted"] = true
		result["changed"] = state.Revision != before.Revision
		httpbridge.JSON(c, 200, result)
		return nil
	}
}

const maxBodyLogDownloadBytes = 1024 * 1024

func DownloadBodyLogs(env *config.EnvConfig, logDir string) fiber.Handler {
	return func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "logs.body.download", "") {
			return nil
		}
		if logger.IsLogDisabled(logDir) {
			httpbridge.JSON(c, 404, fiber.Map{"error": "file logging is disabled"})
			return nil
		}
		if env.LogFile == "" || filepath.Base(env.LogFile) != env.LogFile {
			httpbridge.JSON(c, 400, fiber.Map{"error": "log download requires a regular configured filename"})
			return nil
		}
		path := filepath.Join(logDir, env.LogFile)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			httpbridge.JSON(c, 404, fiber.Map{"error": "log file is unavailable"})
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			httpbridge.JSON(c, 500, fiber.Map{"error": "log file could not be opened"})
			return nil
		}
		defer file.Close()
		opened, err := file.Stat()
		if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			httpbridge.JSON(c, 404, fiber.Map{"error": "log file changed while opening"})
			return nil
		}
		body, err := readCompleteLogLines(file, maxBodyLogDownloadBytes)
		if err != nil {
			httpbridge.JSON(c, 500, fiber.Map{"error": "log file could not be read"})
			return nil
		}
		httpbridge.Header(c, "Content-Disposition", `attachment; filename="body-logs.txt"`)
		httpbridge.Header(c, "Cache-Control", "no-store")
		httpbridge.Header(c, "X-Yunqi-Log-Download-Max-Bytes", strconv.Itoa(maxBodyLogDownloadBytes))
		httpbridge.Header(c, "X-Yunqi-Log-Download-Scope", "current-file-complete-lines")
		httpbridge.Header(c, "X-Yunqi-Log-Download-Truncated", strconv.FormatBool(info.Size() > int64(len(body))))
		httpbridge.Data(c, 200, "text/plain; charset=utf-8", []byte(redaction.String(string(body))))
		return nil
	}
}

// Never expose a fragment of a large/active line: it may cut through a credential
// from a historical log produced before output redaction was installed.
func readCompleteLogLines(reader io.Reader, limit int) ([]byte, error) {
	source := bufio.NewReaderSize(io.LimitReader(reader, int64(limit)*4), 16*1024)
	var result, line []byte
	skip := false
	for {
		part, err := source.ReadSlice('\n')
		if !skip {
			if len(line)+len(part) > limit {
				line = nil
				skip = true
			} else {
				line = append(line, part...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			if err == io.EOF {
				return result, nil
			}
			return nil, err
		}
		if !skip {
			if len(result)+len(line) > limit {
				return result, nil
			}
			result = append(result, line...)
		}
		line = nil
		skip = false
	}
}
