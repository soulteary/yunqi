package routingeval

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	_ "modernc.org/sqlite"
	"path/filepath"
	"reflect"
	"testing"
)

func value[T any](v T) *T { return &v }
func dataset(n int) Dataset {
	d := Dataset{Version: 1, BaselineVersion: "baseline-v1", CandidateVersion: "candidate-v2", FixtureSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", GraderVersion: "rubric-v1", EvidenceSource: "offline_paired"}
	for i := 0; i < n; i++ {
		d.Cases = append(d.Cases, Case{ID: fmt.Sprintf("case-%04d", i), Stratum: "coding", Baseline: Measurement{Quality: value(1.0), CostUSD: value(2.0), LatencyMS: value(100.0), Retries: value(0), Success: value(true)}, Candidate: Measurement{Quality: value(1.0), CostUSD: value(1.0), LatencyMS: value(80.0), Retries: value(0), Success: value(true)}})
	}
	return d
}
func TestPairedEvidenceDoesNotConfuseTransportSuccessWithQuality(t *testing.T) {
	d := dataset(30)
	r, err := Evaluate(d)
	if err != nil {
		t.Fatal(err)
	}
	if r.Decision != "eligible_for_manual_review" || *r.Overall.CostSavedUSD != 30 || *r.Overall.CostSavedPercent != 50 || r.Overall.CandidateP95MS != 80 {
		t.Fatalf("report=%+v", r)
	}
	for i := range d.Cases {
		d.Cases[i].Candidate.Quality = nil
	}
	r, err = Evaluate(d)
	if err != nil {
		t.Fatal(err)
	}
	if r.Decision != "insufficient_evidence" || r.Overall.CandidateQuality != nil {
		t.Fatalf("HTTP success substituted for quality: %+v", r)
	}
}
func TestIndividualGatesAndUnknownCosts(t *testing.T) {
	for _, test := range []struct {
		name, want string
		change     func(*Dataset)
	}{
		{"quality", "rejected", func(d *Dataset) {
			for i := range d.Cases {
				d.Cases[i].Candidate.Quality = value(0.5)
			}
		}},
		{"latency", "rejected", func(d *Dataset) {
			for i := range d.Cases {
				d.Cases[i].Candidate.LatencyMS = value(1000.0)
			}
		}},
		{"retry", "rejected", func(d *Dataset) { d.Cases[0].Candidate.Retries = value(1) }},
		{"success", "rejected", func(d *Dataset) { d.Cases[0].Candidate.Success = value(false) }},
		{"cost", "rejected", func(d *Dataset) {
			for i := range d.Cases {
				d.Cases[i].Candidate.CostUSD = value(4.0)
			}
		}},
		{"unknown_price", "insufficient_evidence", func(d *Dataset) { d.Cases[0].Baseline.CostUSD = nil }},
		{"few_cases", "insufficient_evidence", func(d *Dataset) { d.Cases = d.Cases[:5] }},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := dataset(30)
			test.change(&d)
			r, err := Evaluate(d)
			if err != nil {
				t.Fatal(err)
			}
			if r.Decision != test.want {
				t.Fatalf("got=%s reasons=%v", r.Decision, r.Overall.Reasons)
			}
		})
	}
}
func TestStrataPreventAggregateGainsHidingQualityRegression(t *testing.T) {
	d := dataset(120)
	for i := range d.Cases {
		if i < 30 {
			d.Cases[i].Stratum = "tools"
			d.Cases[i].Candidate.Quality = value(0.8)
		} else {
			d.Cases[i].Baseline.Quality = value(0.5)
		}
	}
	r, err := Evaluate(d)
	if err != nil {
		t.Fatal(err)
	}
	if *r.Overall.CandidateQuality <= *r.Overall.BaselineQuality || r.Decision != "rejected" {
		t.Fatalf("subgroup regression hidden: %+v", r)
	}
}
func TestCanonicalReplayAndValidation(t *testing.T) {
	d := dataset(30)
	r, err := Evaluate(d)
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(d.Cases)-1; i < j; i, j = i+1, j-1 {
		d.Cases[i], d.Cases[j] = d.Cases[j], d.Cases[i]
	}
	replay, err := Evaluate(d)
	if err != nil || !reflect.DeepEqual(r, replay) {
		t.Fatalf("non-deterministic replay: %v", err)
	}
	for _, change := range []func(*Dataset){func(d *Dataset) { d.Cases[0].Candidate.LatencyMS = nil }, func(d *Dataset) { d.Cases[0].Candidate.Quality = value(math.NaN()) }, func(d *Dataset) { d.Cases[0].ID = d.Cases[1].ID }, func(d *Dataset) { d.FixtureSHA256 = "unversioned" }, func(d *Dataset) { d.Gates.MaxQualityLoss = 1 }, func(d *Dataset) { d.Cases[0].Candidate.Retries = value(-1) }} {
		d = dataset(30)
		change(&d)
		if _, err = Evaluate(d); err == nil {
			t.Fatal("invalid evidence accepted")
		}
	}
	raw, _ := json.Marshal(dataset(30))
	raw = append(raw, []byte(` {"prompt":"sensitive"}`)...)
	if _, err = Decode(raw); err == nil {
		t.Fatal("trailing data accepted")
	}
	if _, err = Decode([]byte(`{"version":1,"prompt":"sensitive"}`)); err == nil {
		t.Fatal("unknown sensitive field accepted")
	}
}
func TestPersistenceReplayAfterReopenAndReadOnlyEvaluation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autopilot.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	d := dataset(30)
	saved, err := store.Save(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.Save(context.Background(), d)
	if err != nil || again.CreatedAt != saved.CreatedAt {
		t.Fatalf("idempotency failed: %v", err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err = NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := store.Dataset(context.Background(), saved.Report.ID)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := Evaluate(recovered)
	if err != nil || !reflect.DeepEqual(replay, saved.Report) {
		t.Fatalf("persisted evidence changed: %v", err)
	}
	runs, err := store.List(context.Background())
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs=%v err=%v", runs, err)
	}
	if _, err = store.Get(context.Background(), "' OR 1=1"); err == nil {
		t.Fatal("invalid ID accepted")
	}
}

func TestRetentionKeepsLatestUploadWhenTimestampsTie(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	s, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	var first, last string
	for i := 0; i < RetentionLimit+2; i++ {
		d := dataset(30)
		d.CandidateVersion = fmt.Sprintf("version-%d", i)
		saved, err := s.Save(context.Background(), d)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = saved.Report.ID
		}
		last = saved.Report.ID
	}
	if _, err = s.Get(context.Background(), first); err != sql.ErrNoRows {
		t.Fatalf("oldest report not expired: %v", err)
	}
	runs, err := s.List(context.Background())
	if err != nil || len(runs) != RetentionLimit || runs[0].Report.ID != last {
		t.Fatalf("latest upload lost: %v", err)
	}
}

func TestMeanRetryCountIsNotRetriedRequestRatio(t *testing.T) {
	d := dataset(30)
	for i := 0; i < 15; i++ {
		d.Cases[i].Candidate.Retries = value(3)
	}
	r, err := Evaluate(d)
	if err != nil {
		t.Fatal(err)
	}
	if r.Overall.CandidateMeanRetries != 1.5 {
		t.Fatalf("mean retries=%v", r.Overall.CandidateMeanRetries)
	}
	if r.Decision != "rejected" {
		t.Fatal("retry regression passed")
	}
}
