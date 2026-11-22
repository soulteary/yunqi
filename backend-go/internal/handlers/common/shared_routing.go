package common

import (
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/entrylease"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

type channelAdmissionError struct {
	cause error
	retry time.Duration
}

func (e *channelAdmissionError) Error() string { return "shared channel admission rejected" }
func (e *channelAdmissionError) Unwrap() error { return e.cause }
func acquireSharedChannel(c fiber.Ctx, env *config.EnvConfig, upstream *config.UpstreamConfig) (func(), error) {
	if env.SharedRoutingAdmission == nil || upstream.ChannelUID == "" {
		return nil, &channelAdmissionError{cause: entrylease.ErrUnavailable}
	}
	window := time.Duration(config.RateLimitWindowSeconds(upstream.RateLimitWindowMinutes)) * time.Second
	if window <= 0 {
		window = time.Minute
	}
	ctx, release, retry, err := env.SharedRoutingAdmission.AcquireWindowContext(httpbridge.Request(c).Context(), "channel:"+upstream.ChannelUID, upstream.RateLimitMaxConcurrent, upstream.RateLimitRPM, window)
	if err != nil {
		return nil, &channelAdmissionError{cause: err, retry: retry}
	}
	httpbridge.SetRequest(c, httpbridge.Request(c).WithContext(ctx))
	return release, nil
}
func sharedChannelFailure(err error) (int, string, bool) {
	var admission *channelAdmissionError
	if !errors.As(err, &admission) {
		return 0, "", false
	}
	if errors.Is(err, entrylease.ErrConcurrency) || errors.Is(err, entrylease.ErrRPM) {
		return 429, "channel_capacity_exceeded", true
	}
	return 503, "channel_shared_admission_unavailable", true
}
func sharedChannelRetryHeader(c fiber.Ctx, err error) {
	var admission *channelAdmissionError
	if errors.As(err, &admission) && admission.retry > 0 {
		httpbridge.Header(c, "Retry-After", strconv.Itoa(max(1, int(math.Ceil(admission.retry.Seconds())))))
	}
}
