package scheduler

import (
	"context"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/session"
)

func (s *ChannelScheduler) selectStrictSession(ctx context.Context, opts SelectionOptions) (*SelectionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lease := opts.StrictSession
	opts.StrictSession = nil
	bound, exists := lease.Route()
	if !exists {
		selected, err := s.selectChannelWithOptions(ctx, opts)
		if err != nil {
			return nil, err
		}
		bound, err = lease.Bind(selected.Route)
		if err != nil {
			return nil, err
		}
		if bound.ChannelUID == selected.Route.ChannelUID && bound.Kind == selected.Route.Kind {
			return selected, nil
		}
	}
	original := opts.CandidateFilter
	opts.CandidateFilter = func(channels []ChannelInfo, upstreamFor func(ChannelInfo) *config.UpstreamConfig, available func(ChannelInfo, *config.UpstreamConfig) bool) ([]ChannelInfo, error) {
		if original != nil {
			var err error
			channels, err = original(channels, upstreamFor, available)
			if err != nil {
				return nil, err
			}
		}
		var matches []ChannelInfo
		for _, ch := range channels {
			r := normalizedChannelRoute(ch, opts.Kind)
			if r.ChannelUID == bound.ChannelUID && r.Kind == bound.Kind {
				matches = append(matches, ch)
			}
		}
		if len(matches) == 0 {
			return nil, session.ErrStrictSessionUnavailable
		}
		return matches, nil
	}
	selected, err := s.selectChannelWithOptions(ctx, opts)
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return nil, canceled
		}
		return nil, session.ErrStrictSessionUnavailable
	}
	if selected.Route.ChannelUID != bound.ChannelUID || selected.Route.Kind != bound.Kind {
		return nil, session.ErrStrictSessionUnavailable
	}
	return selected, nil
}
