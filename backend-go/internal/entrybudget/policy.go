// Package entrybudget implements durable caller spending budgets independently
// of provider quota truth and request success/failure statistics.
package entrybudget

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

const NanoPerUSD int64 = 1_000_000_000
const MaxAmount int64 = math.MaxInt64 / 16

var ErrQuota = errors.New("entry budget exceeded")
var ErrUnavailable = errors.New("entry budget storage unavailable")
var ErrClosed = errors.New("entry budget request closed")
var ErrPricing = errors.New("entry budget pricing or output bound unavailable")

type Policy struct {
	PrincipalID       string `json:"principalId"`
	Period            string `json:"period"`
	Timezone          string `json:"timezone"`
	LimitNanoUSD      int64  `json:"limitNanoUsd,string"`
	RequestCapNanoUSD int64  `json:"requestCapNanoUsd,string"`
	RollingSeconds    int64  `json:"rollingSeconds,omitempty"`
	MaxRequestSeconds int64  `json:"maxRequestSeconds,omitempty"`
	Disabled          bool   `json:"disabled,omitempty"`
}

func (p Policy) Normalize() (Policy, error) {
	if p.PrincipalID == "" || len(p.PrincipalID) > 128 || p.LimitNanoUSD < 0 || p.LimitNanoUSD > MaxAmount || p.RequestCapNanoUSD <= 0 || p.RequestCapNanoUSD > MaxAmount {
		return p, errors.New("invalid principal budget amounts")
	}
	if p.Period == "" {
		p.Period = "month"
	}
	if p.Timezone == "" {
		p.Timezone = "UTC"
	}
	if p.Timezone == "Local" {
		return p, errors.New("budget timezone must be explicit")
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		return p, errors.New("invalid budget timezone")
	}
	switch p.Period {
	case "lifetime", "day", "week", "month":
	case "rolling":
		if p.RollingSeconds == 0 {
			p.RollingSeconds = 86400
		}
		if p.RollingSeconds < 1 || p.RollingSeconds > 30*86400 {
			return p, errors.New("invalid rolling budget window")
		}
	default:
		return p, errors.New("invalid budget period")
	}
	if p.MaxRequestSeconds == 0 {
		p.MaxRequestSeconds = 1800
	}
	if p.MaxRequestSeconds < 1 || p.MaxRequestSeconds > 86400 {
		return p, errors.New("invalid budget request lifetime")
	}
	return p, nil
}

type Window struct{ Start, End, Since int64 }

func (p Policy) Window(now time.Time) (Window, error) {
	if p.Period == "lifetime" {
		return Window{}, nil
	}
	if p.Period == "rolling" {
		return Window{Since: now.Unix() - p.RollingSeconds}, nil
	}
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return Window{}, err
	}
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	var end time.Time
	switch p.Period {
	case "day":
		end = start.AddDate(0, 0, 1)
	case "week":
		days := (int(start.Weekday()) + 6) % 7
		start = start.AddDate(0, 0, -days)
		end = start.AddDate(0, 0, 7)
	case "month":
		start = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
		end = start.AddDate(0, 1, 0)
	default:
		return Window{}, errors.New("invalid budget period")
	}
	return Window{Start: start.Unix(), End: end.Unix()}, nil
}
func ParseUSD(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	parts := strings.Split(raw, ".")
	if len(parts) > 2 || len(parts[0]) == 0 || len(parts[0]) > 10 {
		return 0, errors.New("amount must be a nonnegative decimal USD string")
	}
	for _, part := range parts {
		for _, r := range part {
			if r < '0' || r > '9' {
				return 0, errors.New("invalid decimal USD amount")
			}
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > MaxAmount/NanoPerUSD {
		return 0, errors.New("USD amount too large")
	}
	fraction := int64(0)
	if len(parts) == 2 {
		if len(parts[1]) == 0 || len(parts[1]) > 9 {
			return 0, errors.New("USD amount supports up to nine decimals")
		}
		fraction, _ = strconv.ParseInt(parts[1]+strings.Repeat("0", 9-len(parts[1])), 10, 64)
	}
	amount := whole*NanoPerUSD + fraction
	if amount > MaxAmount {
		return 0, errors.New("USD amount too large")
	}
	return amount, nil
}
func FormatUSD(amount int64) string {
	sign := ""
	value := uint64(amount)
	if amount < 0 {
		sign = "-"
		value = uint64(-(amount + 1)) + 1
	}
	return fmt.Sprintf("%s%d.%09d", sign, value/uint64(NanoPerUSD), value%uint64(NanoPerUSD))
}
func FromUSD(value float64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > float64(MaxAmount)/float64(NanoPerUSD) {
		return 0, ErrPricing
	}
	amount := int64(math.Ceil(value * float64(NanoPerUSD)))
	if amount > MaxAmount {
		return 0, ErrPricing
	}
	return amount, nil
}
