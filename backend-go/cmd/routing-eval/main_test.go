package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/soulteary/yunqi/internal/routingeval"
	"os"
	"path/filepath"
	"testing"
)

func p[T any](v T) *T { return &v }
func TestCLIReplaysFixedDatasetAndRejectsInsufficientEvidence(t *testing.T) {
	d := routingeval.Dataset{Version: 1, BaselineVersion: "base", CandidateVersion: "candidate", GraderVersion: "grader-v1", FixtureSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", EvidenceSource: "offline_paired"}
	for i := 0; i < 30; i++ {
		b := routingeval.Measurement{Quality: p(1.0), CostUSD: p(0.02), LatencyMS: p(100.0), Retries: p(0), Success: p(true)}
		c := b
		c.CostUSD = p(0.01)
		d.Cases = append(d.Cases, routingeval.Case{ID: fmt.Sprintf("case-%d", i), Stratum: "coding", Baseline: b, Candidate: c})
	}
	file := filepath.Join(t.TempDir(), "paired.json")
	raw, _ := json.Marshal(d)
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"--input", file, "--check"}, &out); err != nil {
		t.Fatal(err)
	}
	var r routingeval.Report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil || r.Decision != "eligible_for_manual_review" {
		t.Fatalf("output=%s err=%v", out.String(), err)
	}
	d.Cases[0].Candidate.Quality = nil
	raw, _ = json.Marshal(d)
	_ = os.WriteFile(file, raw, 0600)
	out.Reset()
	if err := run([]string{"--input", file, "--check"}, &out); err == nil {
		t.Fatal("missing quality accepted by CI check")
	}
	if !bytes.Contains(out.Bytes(), []byte("quality_labels_missing")) {
		t.Fatal("lost reason for CI rejection")
	}
}
