package common_test

import (
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/conversation"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/racing"
	"github.com/soulteary/yunqi/internal/scheduler"
)

func TestHandleMultiChannelRacingWinnerOwnsConversation(t *testing.T) {
	for _, shadowWins := range []bool{false, true} {
		t.Run(map[bool]string{false: "primary_wins", true: "shadow_wins"}[shadowWins], func(t *testing.T) {
			env := racingTestEnv(t, nil)
			defer env.cleanup()
			installRacingHub(t, racingCandidateList("ch_second"), racing.Behavior{MaxShadows: 1, StreamFloorMs: 30})
			tracker := conversation.NewConversationTracker(time.Minute, time.Hour)
			defer tracker.Stop()
			env.scheduler.SetConversationComponents(tracker, nil)
			c := newRacingFiberContext()
			httpbridge.SetValue(c, "lastUserMessage", "hello")
			httpbridge.SetValue(c, "userMessageCount", 1)
			var winner *scheduler.SelectionResult
			common.HandleMultiChannelFailover(c, &config.EnvConfig{}, env.scheduler, scheduler.ChannelKindMessages, "Messages", "audit-user", "claude-sonnet-4", true, "",
				func(c fiber.Ctx, s *scheduler.SelectionResult) common.MultiChannelAttemptResult {
					if shadowWins && s.Route.ChannelUID == "ch_first" {
						select {
						case <-httpbridge.Request(c).Context().Done():
						case <-time.After(2 * time.Second):
						}
					}
					if !common.RacingClaimClientCommit(c) {
						return common.MultiChannelAttemptResult{Route: s.Route, Attempted: true, LastError: common.ErrRacingSuperseded}
					}
					return common.MultiChannelAttemptResult{Route: s.Route, Attempted: true, Handled: true, SuccessKey: "winner-key"}
				}, func(s *scheduler.SelectionResult, _ common.MultiChannelAttemptResult) { winner = s }, nil)
			wantUID := "ch_first"
			if shadowWins {
				wantUID = "ch_second"
			}
			if winner == nil || winner.Route.ChannelUID != wantUID {
				t.Fatalf("wrong racing winner: %+v", winner)
			}
			conv, ok := tracker.GetConversationByUser("messages", "audit-user")
			if !ok {
				t.Fatal("missing conversation")
			}
			if conv.CurrentChannel != winner.ChannelIndex || conv.ChannelName != winner.Upstream.Name {
				t.Fatalf("conversation channel=%d name=%s; winner route=%+v name=%s", conv.CurrentChannel, conv.ChannelName, winner.Route, winner.Upstream.Name)
			}
			preferred, ok := env.affinity.GetPreferredRoute("messages:audit-user", "messages")
			if !ok || !preferred.Matches(winner.Route) {
				t.Fatalf("conversation affinity differs from winner: %+v", preferred)
			}
		})
	}
}
