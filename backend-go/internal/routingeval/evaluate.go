// Package routingeval evaluates paired, independently measured routing runs.
// It never sends requests, changes a router or interprets HTTP success as quality.
package routingeval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"sort"
)

const MaxCases = 10000

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Measurement struct {
	Quality   *float64 `json:"quality"`
	CostUSD   *float64 `json:"costUsd"`
	LatencyMS *float64 `json:"latencyMs"`
	Retries   *int     `json:"retries"`
	Success   *bool    `json:"success"`
}
type Case struct {
	ID        string      `json:"id"`
	Stratum   string      `json:"stratum"`
	Baseline  Measurement `json:"baseline"`
	Candidate Measurement `json:"candidate"`
}
type Gates struct {
	MinPairs               int     `json:"minPairs"`
	MaxQualityLoss         float64 `json:"maxQualityLoss"`
	MaxCostRatio           float64 `json:"maxCostRatio"`
	MaxP95Ratio            float64 `json:"maxP95Ratio"`
	MaxMeanRetriesIncrease float64 `json:"maxMeanRetriesIncrease"`
}
type Dataset struct {
	Version          int    `json:"version"`
	BaselineVersion  string `json:"baselineVersion"`
	CandidateVersion string `json:"candidateVersion"`
	EvidenceSource   string `json:"evidenceSource"`
	// Content-addressed reference to the fixed prompts, graders and run settings.
	// No prompts, model output, credentials or arbitrary URLs enter this schema.
	FixtureSHA256 string `json:"fixtureSha256"`
	GraderVersion string `json:"graderVersion"`
	Gates         Gates  `json:"gates"`
	Cases         []Case `json:"cases"`
}
type Metrics struct {
	Pairs                     int      `json:"pairs"`
	QualityPairs              int      `json:"qualityPairs"`
	CostPairs                 int      `json:"costPairs"`
	BaselineQuality           *float64 `json:"baselineQuality"`
	CandidateQuality          *float64 `json:"candidateQuality"`
	QualityDeltaLowerApprox95 *float64 `json:"qualityDeltaLowerApprox95"`
	BaselineCostUSD           *float64 `json:"baselineCostUsd"`
	CandidateCostUSD          *float64 `json:"candidateCostUsd"`
	CostSavedUSD              *float64 `json:"costSavedUsd"`
	CostSavedPercent          *float64 `json:"costSavedPercent"`
	BaselineP95MS             float64  `json:"baselineP95Ms"`
	CandidateP95MS            float64  `json:"candidateP95Ms"`
	BaselineMeanRetries       float64  `json:"baselineMeanRetries"`
	CandidateMeanRetries      float64  `json:"candidateMeanRetries"`
	BaselineSuccessRate       float64  `json:"baselineSuccessRate"`
	CandidateSuccessRate      float64  `json:"candidateSuccessRate"`
	Decision                  string   `json:"decision"`
	Reasons                   []string `json:"reasons"`
}
type Stratum struct {
	Name    string  `json:"name"`
	Metrics Metrics `json:"metrics"`
}
type Report struct {
	ID               string    `json:"id"`
	DatasetSHA256    string    `json:"datasetSha256"`
	FixtureSHA256    string    `json:"fixtureSha256"`
	BaselineVersion  string    `json:"baselineVersion"`
	CandidateVersion string    `json:"candidateVersion"`
	GraderVersion    string    `json:"graderVersion"`
	EvidenceSource   string    `json:"evidenceSource"`
	Gates            Gates     `json:"gates"`
	Overall          Metrics   `json:"overall"`
	Strata           []Stratum `json:"strata"`
	Decision         string    `json:"decision"`
	// Descriptive paired-sample interval; no automatic rollout or causal claim.
	IntervalMethod string `json:"intervalMethod"`
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func validMeasurement(m Measurement) bool {
	if m.LatencyMS == nil || m.Retries == nil || m.Success == nil {
		return false
	}
	if !finite(*m.LatencyMS) || *m.LatencyMS < 0 || *m.LatencyMS > 86400000 || *m.Retries < 0 || *m.Retries > 1000 {
		return false
	}
	if m.Quality != nil && (!finite(*m.Quality) || *m.Quality < 0 || *m.Quality > 1) {
		return false
	}
	if m.CostUSD != nil && (!finite(*m.CostUSD) || *m.CostUSD < 0 || *m.CostUSD > 1e6) {
		return false
	}
	return true
}
func defaults(g Gates) Gates {
	if g.MinPairs == 0 {
		g.MinPairs = 30
	}
	if g.MaxCostRatio == 0 {
		g.MaxCostRatio = 1
	}
	if g.MaxP95Ratio == 0 {
		g.MaxP95Ratio = 1.1
	}
	return g
}
func Evaluate(d Dataset) (Report, error) {
	g := defaults(d.Gates)
	if d.Version != 1 || !identifier.MatchString(d.BaselineVersion) || !identifier.MatchString(d.CandidateVersion) || d.BaselineVersion == d.CandidateVersion || !identifier.MatchString(d.GraderVersion) || len(d.Cases) == 0 || len(d.Cases) > MaxCases {
		return Report{}, errors.New("invalid evaluation dataset")
	}
	hash, e := hex.DecodeString(d.FixtureSHA256)
	if e != nil || len(hash) != 32 {
		return Report{}, errors.New("fixture SHA-256 is required")
	}
	if d.EvidenceSource != "offline_paired" && d.EvidenceSource != "shadow_paired" {
		return Report{}, errors.New("paired measurement evidence is required")
	}
	if g.MinPairs < 30 || g.MinPairs > MaxCases || !finite(g.MaxQualityLoss) || g.MaxQualityLoss < 0 || g.MaxQualityLoss > 0.1 || !finite(g.MaxCostRatio) || g.MaxCostRatio < 0.01 || g.MaxCostRatio > 2 || !finite(g.MaxP95Ratio) || g.MaxP95Ratio < 0.01 || g.MaxP95Ratio > 2 || !finite(g.MaxMeanRetriesIncrease) || g.MaxMeanRetriesIncrease < 0 || g.MaxMeanRetriesIncrease > 1 {
		return Report{}, errors.New("invalid evaluation gates")
	}
	seen := map[string]bool{}
	strata := map[string][]Case{}
	for _, c := range d.Cases {
		if !identifier.MatchString(c.ID) || seen[c.ID] || !identifier.MatchString(c.Stratum) || !validMeasurement(c.Baseline) || !validMeasurement(c.Candidate) {
			return Report{}, errors.New("invalid or duplicate paired case")
		}
		seen[c.ID] = true
		strata[c.Stratum] = append(strata[c.Stratum], c)
	}
	if len(strata) > 64 {
		return Report{}, errors.New("too many evaluation strata")
	}
	// Canonical order makes replay content-addressed regardless of upload order.
	d.Gates = g
	d.Cases = append([]Case(nil), d.Cases...)
	sort.Slice(d.Cases, func(i, j int) bool { return d.Cases[i].ID < d.Cases[j].ID })
	strata = map[string][]Case{}
	for _, c := range d.Cases {
		strata[c.Stratum] = append(strata[c.Stratum], c)
	}
	raw, _ := json.Marshal(d)
	sum := sha256.Sum256(raw)
	id := hex.EncodeToString(sum[:])
	r := Report{ID: id, DatasetSHA256: id, FixtureSHA256: d.FixtureSHA256, BaselineVersion: d.BaselineVersion, CandidateVersion: d.CandidateVersion, GraderVersion: d.GraderVersion, EvidenceSource: d.EvidenceSource, Gates: g, Overall: summarize(d.Cases, g), Decision: "eligible_for_manual_review", IntervalMethod: "paired mean delta minus 2.045 standard errors; descriptive, not causal or automatic rollout"}
	names := make([]string, 0, len(strata))
	for name := range strata {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		m := summarize(strata[name], g)
		r.Strata = append(r.Strata, Stratum{Name: name, Metrics: m})
		r.Decision = combine(r.Decision, m.Decision)
	}
	r.Decision = combine(r.Decision, r.Overall.Decision)
	return r, nil
}
func combine(a, b string) string {
	if a == "rejected" || b == "rejected" {
		return "rejected"
	}
	if a == "insufficient_evidence" || b == "insufficient_evidence" {
		return "insufficient_evidence"
	}
	return "eligible_for_manual_review"
}
func ptr(v float64) *float64  { return &v }
func p95(v []float64) float64 { sort.Float64s(v); return v[int(math.Ceil(0.95*float64(len(v))))-1] }
func summarize(cases []Case, g Gates) Metrics {
	m := Metrics{Pairs: len(cases), Decision: "eligible_for_manual_review", Reasons: []string{}}
	var bq, cq, bc, cc, br, cr, bs, cs float64
	var delta, bl, cl []float64
	for _, c := range cases {
		b, a := c.Baseline, c.Candidate
		bl = append(bl, *b.LatencyMS)
		cl = append(cl, *a.LatencyMS)
		br += float64(*b.Retries)
		cr += float64(*a.Retries)
		if *b.Success {
			bs++
		}
		if *a.Success {
			cs++
		}
		if b.Quality != nil && a.Quality != nil {
			m.QualityPairs++
			bq += *b.Quality
			cq += *a.Quality
			delta = append(delta, *a.Quality-*b.Quality)
		}
		if b.CostUSD != nil && a.CostUSD != nil {
			m.CostPairs++
			bc += *b.CostUSD
			cc += *a.CostUSD
		}
	}
	n := float64(len(cases))
	m.BaselineP95MS = p95(bl)
	m.CandidateP95MS = p95(cl)
	m.BaselineMeanRetries = br / n
	m.CandidateMeanRetries = cr / n
	m.BaselineSuccessRate = bs / n
	m.CandidateSuccessRate = cs / n
	insufficient := func(reason string) {
		m.Decision = combine(m.Decision, "insufficient_evidence")
		m.Reasons = append(m.Reasons, reason)
	}
	reject := func(reason string) { m.Decision = "rejected"; m.Reasons = append(m.Reasons, reason) }
	if m.Pairs < g.MinPairs {
		insufficient("sample_count_below_minimum")
	}
	if m.QualityPairs != m.Pairs {
		insufficient("quality_labels_missing")
	}
	if m.CostPairs != m.Pairs {
		insufficient("cost_measurements_missing")
	}
	if m.QualityPairs > 0 {
		qn := float64(m.QualityPairs)
		m.BaselineQuality = ptr(bq / qn)
		m.CandidateQuality = ptr(cq / qn)
		mean := (cq - bq) / qn
		if len(delta) > 1 {
			variance := 0.0
			for _, v := range delta {
				variance += (v - mean) * (v - mean)
			}
			lower := mean - 2.045*math.Sqrt(variance/(qn-1)/qn)
			m.QualityDeltaLowerApprox95 = ptr(lower)
			if mean < -g.MaxQualityLoss-1e-12 {
				reject("observed_quality_regression")
			} else if lower < -g.MaxQualityLoss-1e-12 {
				insufficient("quality_noninferiority_not_established")
			}
		} else {
			insufficient("quality_interval_unavailable")
		}
	}
	if m.CostPairs > 0 {
		m.BaselineCostUSD = ptr(bc)
		m.CandidateCostUSD = ptr(cc)
		m.CostSavedUSD = ptr(bc - cc)
		if bc > 0 {
			m.CostSavedPercent = ptr(100 * (bc - cc) / bc)
		}
		if cc > bc*g.MaxCostRatio+1e-12 {
			reject("cost_gate_failed")
		}
	}
	if m.CandidateP95MS > m.BaselineP95MS*g.MaxP95Ratio+1e-9 {
		reject("latency_gate_failed")
	}
	if m.CandidateMeanRetries > m.BaselineMeanRetries+g.MaxMeanRetriesIncrease+1e-12 {
		reject("retry_gate_failed")
	}
	if cs < bs {
		reject("success_rate_regression")
	}
	return m
}
