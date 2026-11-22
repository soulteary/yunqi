package common

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/utils"
	"strings"
)

func acquireStrictSession(c fiber.Ctx, env *config.EnvConfig, s *scheduler.ChannelScheduler, kind scheduler.ChannelKind, userID, role string) (*session.StrictLease, bool) {
	if !env.StrictSessionAffinity {
		return nil, true
	}
	principal := middleware.GetPrincipalID(c)
	if utils.ExtractExplicitSessionID(c, GetEffectiveRequestBody(c, nil)) == "" || principal == "" || strings.TrimSpace(userID) == "" || len(userID) > 1024 {
		httpbridge.JSON(c, 400, fiber.Map{"error": "strict_session_identity_required"})
		return nil, false
	}
	if role != "subagent" {
		role = "main"
	}
	raw, _ := json.Marshal([]string{principal, string(kind), role, userID})
	sum := sha256.Sum256(raw)
	lease, err := s.GetTraceAffinityManager().AcquireStrict(hex.EncodeToString(sum[:]))
	if err != nil {
		code := "strict_session_unavailable"
		if errors.Is(err, session.ErrStrictSessionCapacity) {
			code = "strict_session_capacity"
		}
		httpbridge.JSON(c, 503, fiber.Map{"error": code})
		return nil, false
	}
	httpbridge.Header(c, "X-Yunqi-Session-Affinity", "strict")
	return lease, true
}
