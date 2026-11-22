package entrybudget

import (
	"math"
	"testing"
	"time"
)

func TestBudgetDecimalMoneyIsExactAndRejectsCoercion(t *testing.T) {
	for raw, want := range map[string]int64{"0": 0, "0.000000001": 1, "1.23": 1230000000, "100.000000009": 100000000009} {
		got, err := ParseUSD(raw)
		if err != nil || got != want {
			t.Fatalf("%s=%d %v", raw, got, err)
		}
	}
	for _, raw := range []string{"-1", "+1", "NaN", "1e3", "0.0000000001", "1.", "", "99999999999999"} {
		if _, err := ParseUSD(raw); err == nil {
			t.Fatalf("coerced malformed amount %q", raw)
		}
	}
	if FormatUSD(1) != "0.000000001" || FormatUSD(-1) != "-0.000000001" {
		t.Fatal("nanoUSD formatting lost precision")
	}
	if _, err := FromUSD(math.Inf(1)); err == nil {
		t.Fatal("infinite usage cost accepted")
	}
}
func TestCalendarBudgetResetsRespectTimezoneAndDST(t *testing.T) {
	p := Policy{PrincipalID: "team", Period: "day", Timezone: "America/New_York", LimitNanoUSD: 10, RequestCapNanoUSD: 10}
	p, err := p.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		date  string
		hours int64
	}{{"2024-03-10T12:00:00Z", 23}, {"2024-11-03T12:00:00Z", 25}} {
		now, _ := time.Parse(time.RFC3339, tc.date)
		w, err := p.Window(now)
		if err != nil || w.End-w.Start != tc.hours*3600 {
			t.Fatalf("DST window %s %+v %v", tc.date, w, err)
		}
	}
	p.Period = "month"
	p.Timezone = "Asia/Shanghai"
	before, _ := time.Parse(time.RFC3339, "2026-11-30T15:59:59Z")
	after := before.Add(time.Second)
	first, _ := p.Window(before)
	second, _ := p.Window(after)
	if first.End != second.Start || first.Start == second.Start {
		t.Fatal("month did not reset at configured local midnight")
	}
}
