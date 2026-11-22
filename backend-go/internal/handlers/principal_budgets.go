package handlers

import (
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/entrybudget"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"strings"
	"time"
)

type budgetPolicyView struct {
	PrincipalID       string `json:"principalId"`
	Period            string `json:"period"`
	Timezone          string `json:"timezone"`
	LimitUSD          string `json:"limitUsd"`
	RequestCapUSD     string `json:"requestCapUsd"`
	RollingSeconds    int64  `json:"rollingSeconds,omitempty"`
	MaxRequestSeconds int64  `json:"maxRequestSeconds"`
	Disabled          bool   `json:"disabled"`
}

func publicBudget(p entrybudget.Policy) budgetPolicyView {
	return budgetPolicyView{p.PrincipalID, p.Period, p.Timezone, entrybudget.FormatUSD(p.LimitNanoUSD), entrybudget.FormatUSD(p.RequestCapNanoUSD), p.RollingSeconds, p.MaxRequestSeconds, p.Disabled}
}
func budgetState(c fiber.Ctx, env *config.EnvConfig, p entrybudget.Policy, audit bool) error {
	if env.BudgetManager == nil {
		httpbridge.JSON(c, 503, fiber.Map{"error": "entry_budget_unavailable"})
		return nil
	}
	store, err := env.BudgetManager.Store()
	if err != nil {
		httpbridge.JSON(c, 503, fiber.Map{"error": "entry_budget_unavailable"})
		return nil
	}
	state, err := store.Snapshot(httpbridge.Request(c).Context(), p, time.Now())
	if err != nil {
		httpbridge.JSON(c, 503, fiber.Map{"error": "entry_budget_unavailable"})
		return nil
	}
	response := fiber.Map{"costBasis": "list_price_usd", "budgeted": !p.Disabled, "reservationMethod": "serialized_input_byte_bound_plus_output_limit", "absoluteCostGuarantee": false, "policy": publicBudget(p), "periodStart": state.Start, "periodEnd": state.End, "spentUsd": entrybudget.FormatUSD(state.Spent), "reservedUsd": entrybudget.FormatUSD(state.Reserved), "remainingUsd": entrybudget.FormatUSD(p.LimitNanoUSD - state.Spent - state.Reserved)}
	if audit {
		requests, err := store.Requests(httpbridge.Request(c).Context(), p.PrincipalID, 100)
		if err != nil {
			httpbridge.JSON(c, 503, fiber.Map{"error": "entry_budget_unavailable"})
			return nil
		}
		response["requests"] = requests
	}
	httpbridge.JSON(c, 200, response)
	return nil
}
func RegisterPrincipalBudgetRoutes(router fiber.Router, env *config.EnvConfig, cm *config.ConfigManager) {
	router.Get("/budgets", func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "budgets.read", "") {
			return nil
		}
		policies := make([]budgetPolicyView, 0)
		for _, p := range cm.PrincipalBudgetsSnapshot() {
			policies = append(policies, publicBudget(p))
		}
		httpbridge.JSON(c, 200, fiber.Map{"costBasis": "list_price_usd", "budgets": policies})
		return nil
	})
	router.Put("/budgets", func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "budgets.configure", "") {
			return nil
		}
		var v budgetPolicyView
		if httpbridge.BindJSON(c, &v) != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_budget_policy"})
			return nil
		}
		limit, e1 := entrybudget.ParseUSD(v.LimitUSD)
		cap, e2 := entrybudget.ParseUSD(v.RequestCapUSD)
		p := entrybudget.Policy{PrincipalID: v.PrincipalID, Period: v.Period, Timezone: v.Timezone, LimitNanoUSD: limit, RequestCapNanoUSD: cap, RollingSeconds: v.RollingSeconds, MaxRequestSeconds: v.MaxRequestSeconds, Disabled: v.Disabled}
		normalized, err := p.Normalize()
		if e1 != nil || e2 != nil || err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_budget_policy"})
			return nil
		}
		if cm.PutPrincipalBudget(normalized) != nil {
			httpbridge.JSON(c, 500, fiber.Map{"error": "budget_policy_save_failed"})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{"costBasis": "list_price_usd", "policy": publicBudget(normalized)})
		return nil
	})
	router.Get("/budgets/:principal", func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "budgets.read", "") {
			return nil
		}
		p, ok := cm.FindPrincipalBudget("principal:" + httpbridge.Param(c, "principal"))
		if !ok {
			httpbridge.JSON(c, 404, fiber.Map{"error": "budget_not_found"})
			return nil
		}
		return budgetState(c, env, p, true)
	})
	router.Get("/budgets/:principal/requests/:request", func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "budgets.read", "") {
			return nil
		}
		principal := httpbridge.Param(c, "principal")
		if _, ok := cm.FindPrincipalBudget("principal:" + principal); !ok {
			httpbridge.JSON(c, 404, fiber.Map{"error": "budget_not_found"})
			return nil
		}
		if env.BudgetManager == nil {
			httpbridge.JSON(c, 503, fiber.Map{"error": "entry_budget_unavailable"})
			return nil
		}
		store, err := env.BudgetManager.Store()
		if err != nil {
			return err
		}
		// A request ID is never authority; query its owner directly without a
		// recent-history limit that would make old legitimate records inaccessible.
		id := httpbridge.Param(c, "request")
		found, err := store.RequestOwned(httpbridge.Request(c).Context(), id, principal)
		if err != nil {
			return err
		}

		if !found {
			httpbridge.JSON(c, 404, fiber.Map{"error": "budget_request_not_found"})
			return nil
		}
		attempts, err := store.Attempts(httpbridge.Request(c).Context(), id)
		if err != nil {
			return err
		}
		httpbridge.JSON(c, 200, fiber.Map{"attempts": attempts})
		return nil
	})
}
func CallerBudgetHandler(env *config.EnvConfig, cm *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		principal := middleware.GetPrincipalID(c)
		p, ok := cm.FindPrincipalBudget(principal)
		if !ok {
			httpbridge.JSON(c, 200, fiber.Map{"costBasis": "list_price_usd", "budgeted": false, "stablePrincipal": strings.HasPrefix(principal, "principal:")})
			return nil
		}
		return budgetState(c, env, p, false)
	}
}
