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
	// Each attempt replaces the prior attempt diagnostic, including native paths.
	httpbridge.Writer(c).Header().Del("X-Yunqi-Conversion")
	httpbridge.SetValue(c, conversionReportKey, nil)
	from = conversionProtocol(from)
	target = conversionProtocol(target)
	if request != nil {
		path := strings.TrimRight(strings.ToLower(request.URL.Path), "/")
		switch {
		case strings.HasSuffix(path, "/chat/completions"):
			target = "chat"
		case strings.HasSuffix(path, "/responses"):
			target = "responses"
		case strings.HasSuffix(path, "/messages"):
			target = "messages"
		case strings.HasSuffix(path, ":generatecontent") || strings.HasSuffix(path, ":streamgeneratecontent"):
			target = "gemini"
		}
	}
	if from == target {
		return nil
	}
	if from != "responses" && from != "messages" && from != "chat" && from != "gemini" {
		return nil
	}
	publish := func(report converters.ConversionReport) {
		httpbridge.SetValue(c, conversionReportKey, report)
		if raw, err := json.Marshal(report); err == nil {
			httpbridge.Header(c, "X-Yunqi-Conversion", string(raw))
		}
	}
	body := []byte(`{}`)
	if request != nil {
		if request.GetBody == nil {
			report, _ := converters.AnalyzeConversion(source, nil, from, target)
			report.Diagnostics = []converters.ConversionDiagnostic{{Field: "request", Action: "unverified", Code: "wire_body_not_replayable"}}
			publish(report)
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
	report, err := converters.AnalyzeConversion(source, body, from, target)
	if request == nil && err == nil {
		if report.Quality != "unknown" {
			report.Quality = "lossy"
		}
		report.Diagnostics = append(report.Diagnostics, converters.ConversionDiagnostic{Field: "request", Action: "rejected", Code: "conversion_failed"})
	}
	publish(report)
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
