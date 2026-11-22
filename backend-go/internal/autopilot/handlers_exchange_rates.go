package autopilot

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

type exchangeRatesResponse struct {
	Quotes   []config.ExchangeRateQuote   `json:"quotes"`
	Snapshot *config.ExchangeRateSnapshot `json:"snapshot,omitempty"`
	Source   string                       `json:"source,omitempty"`
}

type exchangeRatesUpdateRequest struct {
	Quotes                  []config.ExchangeRateQuote `json:"quotes"`
	ExpectedSnapshotVersion *uint64                    `json:"expectedSnapshotVersion,omitempty"`
}

type exchangeRatesUpdateResponse struct {
	Quotes   []config.ExchangeRateQuote   `json:"quotes"`
	Snapshot *config.ExchangeRateSnapshot `json:"snapshot"`
	Source   string                       `json:"source,omitempty"`
	Version  uint64                       `json:"version"`
}

func RegisterCostRoutes(router fiber.Router, cfgManager *config.ConfigManager) {
	if router == nil || cfgManager == nil {
		return
	}
	group := router.Group("/autopilot/cost")
	group.Get("/exchange-rates", handleGetExchangeRates(cfgManager))
	group.Put("/exchange-rates", handlePutExchangeRates(cfgManager))
}

func handleGetExchangeRates(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		cfg := cfgManager.GetConfig()
		cost := cfg.AutopilotRouting.CostOptimization
		response := exchangeRatesResponse{
			Quotes: append([]config.ExchangeRateQuote(nil), cost.ExchangeRateQuotes...),
			Source: strings.TrimSpace(cost.ExchangeRateSource),
		}
		if cost.ExchangeRateSnapshot != nil {
			response.Snapshot = cost.ExchangeRateSnapshot
		} else if graph, err := config.NewExchangeRateGraph(cost.ExchangeRateQuotes, 0, time.Now().UTC()); err == nil {
			snapshot := graph.Snapshot()
			response.Snapshot = &snapshot
		}
		httpbridge.JSON(c, http.StatusOK, response)
		return nil
	}
}

func handlePutExchangeRates(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		body, err := readExchangeRatesUpdateRequest(c)
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		cfg := cfgManager.GetConfig()
		current := cfg.AutopilotRouting.CostOptimization
		expected := body.ExpectedSnapshotVersion
		if expected != nil {
			currentVersion := currentSnapshotVersion(current.ExchangeRateSnapshot)
			if currentVersion != *expected {
				httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": fmt.Sprintf("snapshot version 冲突: current=%d expected=%d", currentVersion, *expected)})
				return nil
			}
		}

		normalized, snapshot, err := validateExchangeRates(body.Quotes, currentSnapshotVersion(current.ExchangeRateSnapshot), time.Now().UTC())
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}

		updated, err := cfgManager.ApplyExchangeRateUpdateAudited(managementAuditContext, normalized, *snapshot, body.Quotes != nil && len(body.Quotes) == 0)
		if err != nil {
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, exchangeRatesUpdateResponse{
			Quotes:   updated.Quotes,
			Snapshot: updated.Snapshot,
			Source:   updated.Source,
			Version:  updated.Snapshot.Version,
		})
		return nil
	}
}

func readExchangeRatesUpdateRequest(c fiber.Ctx) (exchangeRatesUpdateRequest, error) {
	data, err := httpbridge.ReadBody(c)
	if err != nil {
		return exchangeRatesUpdateRequest{}, fmt.Errorf("读取请求体失败: %w", err)
	}
	if len(data) == 0 {
		return exchangeRatesUpdateRequest{}, errors.New("请求体不能为空")
	}
	var raw struct {
		Quotes                  []json.RawMessage `json:"quotes"`
		ExpectedSnapshotVersion *uint64           `json:"expectedSnapshotVersion"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return exchangeRatesUpdateRequest{}, fmt.Errorf("请求参数无效: %w", err)
	}
	if raw.Quotes == nil {
		return exchangeRatesUpdateRequest{}, errors.New("quotes 字段不能省略；显式置空请传 []")
	}
	quotes := make([]config.ExchangeRateQuote, 0, len(raw.Quotes))
	for i, item := range raw.Quotes {
		var quote config.ExchangeRateQuote
		if err := json.Unmarshal(item, &quote); err != nil {
			return exchangeRatesUpdateRequest{}, fmt.Errorf("quotes[%d] 解析失败: %w", i, err)
		}
		quotes = append(quotes, quote)
	}
	return exchangeRatesUpdateRequest{Quotes: quotes, ExpectedSnapshotVersion: raw.ExpectedSnapshotVersion}, nil
}

func validateExchangeRates(quotes []config.ExchangeRateQuote, currentVersion uint64, now time.Time) ([]config.ExchangeRateQuote, *config.ExchangeRateSnapshot, error) {
	graph, err := config.NewExchangeRateGraph(quotes, currentVersion, now)
	if err != nil {
		return nil, nil, fmt.Errorf("汇率图验证失败: %w", err)
	}
	if err := graph.ReplaceQuotes(quotes, now); err != nil {
		return nil, nil, fmt.Errorf("汇率图验证失败: %w", err)
	}
	snapshot := graph.Snapshot()
	return graph.NormalizedQuotes(), &snapshot, nil
}

func currentSnapshotVersion(snapshot *config.ExchangeRateSnapshot) uint64 {
	if snapshot == nil {
		return 0
	}
	return snapshot.Version
}

// parseUint64Param 辅助处理路径或查询参数中的无符号 64 位整数。
func parseUint64Param(value string) (uint64, error) {
	if value == "" {
		return 0, errors.New("空参数")
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}
