package common

import (
	"encoding/json"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/converters"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"io"
	"net/http"
	"strings"
)

const conversionReportKey = "protocolConversionReport"

func recordFinalConversion(c fiber.Ctx, source []byte, request *http.Request, from, target string) error {
	from = conversionProtocol(from)
	target = conversionProtocol(target)
	body := []byte(`{}`)
	if request != nil {
		path := strings.ToLower(request.URL.Path)
		switch {
		case strings.Contains(path, "chat/completions"):
			target = "chat"
		case strings.Contains(path, "/responses"):
			target = "responses"
		case strings.Contains(path, "/messages"):
			target = "messages"
		case strings.Contains(path, "generatecontent"):
			target = "gemini"
		}
		if request.GetBody == nil {
			return nil
		}
		reader, err := request.GetBody()
		if err != nil {
			return err
		}
		body, err = io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			return err
		}
	}
	if from == target {
		return nil
	}
	if from != "responses" && from != "messages" && from != "chat" && from != "gemini" {
		return nil
	}
	report, err := converters.AnalyzeConversion(source, body, from, target)
	if request == nil && err == nil {
		report.Quality = "lossy"
		report.Diagnostics = append(report.Diagnostics, converters.ConversionDiagnostic{Field: "request", Action: "rejected", Code: "conversion_failed"})
	}
	httpbridge.SetValue(c, conversionReportKey, report)
	raw, marshalErr := json.Marshal(report)
	if marshalErr == nil {
		httpbridge.Header(c, "X-Yunqi-Conversion", string(raw))
	}
	return err
}

func conversionProtocol(value string) string {
	switch strings.ToLower(value) {
	case "claude", "messages":
		return "messages"
	case "openai", "chat":
		return "chat"
	case "copilot", "responses":
		return "responses"
	case "gemini":
		return "gemini"
	default:
		return strings.ToLower(value)
	}
}
