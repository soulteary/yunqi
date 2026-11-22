package presetstore

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/soulteary/yunqi/internal/pricingtypes"
)

const priceReviewFile = "price-review.json"
const maxPriceHistoryBytes = 16 << 20
const maxPriceReviewBytes = 32 << 20

var ErrPriceReviewConflict = errors.New("price review is stale; refresh before approving")
var ErrPriceAnomalyAck = errors.New("price anomalies require explicit acknowledgement")
var ErrPriceRollbackUnavailable = errors.New("price rollback snapshot is unavailable")

type PriceChange struct {
	Model   string                `json:"model"`
	Before  *pricingtypes.Pricing `json:"before,omitempty"`
	After   *pricingtypes.Pricing `json:"after,omitempty"`
	Anomaly bool                  `json:"anomaly"`
	Reasons []string              `json:"reasons,omitempty"`
}
type PriceProposal struct {
	PolicyError                    string        `json:"policyError,omitempty"`
	SourceKind                     string        `json:"sourceKind"`
	SourcePublishedAt              *time.Time    `json:"sourcePublishedAt,omitempty"`
	SourceCheckedAt                time.Time     `json:"sourceCheckedAt"`
	BeforeBundleHash               string        `json:"beforeBundleHash"`
	PreviewTruncated               bool          `json:"previewTruncated"`
	ReviewID                       string        `json:"reviewId"`
	BeforeVersion                  string        `json:"beforeVersion"`
	AfterVersion                   string        `json:"afterVersion"`
	DataVersion                    string        `json:"dataVersion"`
	SourceURL                      string        `json:"sourceUrl"`
	SourceDigest                   string        `json:"sourceDigest"`
	SigningKeyID                   string        `json:"signingKeyId,omitempty"`
	SignatureVerified              bool          `json:"signatureVerified"`
	CreatedAt                      time.Time     `json:"createdAt"`
	ThresholdPercent               float64       `json:"thresholdPercent"`
	ChangeCount                    int           `json:"changeCount"`
	Changes                        []PriceChange `json:"changes"`
	RequiresAnomalyAcknowledgement bool          `json:"requiresAnomalyAcknowledgement"`
}
type PriceRevision struct {
	SourcePublishedAt *time.Time `json:"sourcePublishedAt,omitempty"`
	SourceCheckedAt   *time.Time `json:"sourceCheckedAt,omitempty"`
	SourceDigest      string     `json:"sourceDigest,omitempty"`
	SigningKeyID      string     `json:"signingKeyId,omitempty"`
	SignatureVerified bool       `json:"signatureVerified"`
	VersionID         string     `json:"versionId"`
	DataVersion       string     `json:"dataVersion"`
	BundleHash        string     `json:"bundleHash"`
	At                time.Time  `json:"at"`
	ActorID           string     `json:"actorId"`
}
type PriceAudit struct {
	At           time.Time `json:"at"`
	ActorID      string    `json:"actorId"`
	Action       string    `json:"action"`
	VersionID    string    `json:"versionId"`
	SourceDigest string    `json:"sourceDigest,omitempty"`
}
type PriceReviewState struct {
	RollbackBlockedThrough string          `json:"rollbackBlockedThrough,omitempty"`
	LastCheckError         string          `json:"lastCheckError,omitempty"`
	LastCheckAt            *time.Time      `json:"lastCheckAt,omitempty"`
	SignatureRequired      bool            `json:"signatureRequired"`
	SourceMaxAge           string          `json:"sourceMaxAge"`
	SourceFutureSkew       string          `json:"sourceFutureSkew"`
	ActivePriceSource      *PriceRevision  `json:"activePriceSource,omitempty"`
	ActiveVersion          string          `json:"activeVersion"`
	ActiveDataVersion      string          `json:"activeDataVersion"`
	ActiveBundleHash       string          `json:"activeBundleHash"`
	Pending                *PriceProposal  `json:"pending,omitempty"`
	History                []PriceRevision `json:"history"`
	Audit                  []PriceAudit    `json:"audit"`
	RollbackScope          string          `json:"rollbackScope"`
}
type priceHistory struct {
	Revision PriceRevision `json:"revision"`
	Bundle   *PresetBundle `json:"bundle"`
}
type pricePending struct {
	SourceIndex *PresetIndex  `json:"sourceIndex"`
	Proposal    PriceProposal `json:"proposal"`
	Bundle      *PresetBundle `json:"bundle"`
}
type presetCacheReceipt struct {
	BundleHash     string        `json:"bundleHash"`
	PricingVersion string        `json:"pricingVersion"`
	SourceIndex    *PresetIndex  `json:"sourceIndex"`
	Revision       PriceRevision `json:"revision"`
}
type priceReviewDisk struct {
	CachedSource           *presetCacheReceipt `json:"cachedSource,omitempty"`
	RollbackBlockedThrough string              `json:"rollbackBlockedThrough,omitempty"`
	SchemaVersion          int                 `json:"schemaVersion"`
	Pending                *pricePending       `json:"pending,omitempty"`
	History                []priceHistory      `json:"history,omitempty"`
	Audit                  []PriceAudit        `json:"audit,omitempty"`
	ApprovedBundleHash     string              `json:"approvedBundleHash,omitempty"`
}

func ParseTrustedPricingKeys(value string) (map[string]ed25519.PublicKey, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var encoded map[string]string
	if json.Unmarshal([]byte(value), &encoded) != nil || encoded == nil || len(encoded) > 16 {
		return nil, errors.New("invalid preset signing public keys")
	}
	keys := map[string]ed25519.PublicKey{}
	for id, value := range encoded {
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(decoded) != ed25519.PublicKeySize || id == "" || len(id) > 64 {
			return nil, errors.New("invalid preset signing public key")
		}
		keys[id] = ed25519.PublicKey(decoded)
	}
	return keys, nil
}
func (u *PresetUpdater) verifyIndexSignature(index *PresetIndex) (bool, error) {
	if u.config.SigningConfigError != "" {
		return false, errors.New(u.config.SigningConfigError)
	}
	if index == nil {
		return false, errors.New("missing preset source index")
	}
	if index.Signature == "" {
		if len(u.config.TrustedSigningKeys) > 0 {
			return false, errors.New("a trusted preset signing key is configured; unsigned indexes are rejected")
		}
		if index.SigningKeyID != "" {
			return false, errors.New("preset index signing key has no signature")
		}
		return false, nil
	}
	key := u.config.TrustedSigningKeys[index.SigningKeyID]
	if len(key) != ed25519.PublicKeySize {
		return false, errors.New("preset index signing key is not trusted")
	}
	signature, err := base64.StdEncoding.DecodeString(index.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return false, errors.New("invalid preset index signature encoding")
	}
	copy := *index
	copy.PublishedAt = copy.PublishedAt.UTC()
	copy.Signature = ""
	payload, err := json.Marshal(copy)
	if err != nil || !ed25519.Verify(key, payload, signature) {
		return false, errors.New("preset index signature verification failed")
	}
	return true, nil
}
func pricingMap(bundle *PresetBundle) (map[string]*pricingtypes.Pricing, error) {
	out := map[string]*pricingtypes.Pricing{}
	if bundle == nil {
		return nil, errors.New("missing preset bundle")
	}
	registry := bundle.ModelRegistry
	if registry == nil || len(registry.UpstreamCapabilities) == 0 {
		registry = EmbeddedBundle().ModelRegistry
	}
	if registry == nil {
		return nil, errors.New("missing builtin pricing fallback")
	}
	size, count := 0, 0
	for _, capability := range registry.UpstreamCapabilities {
		pricing := pricingtypes.Snapshot(capability.Pricing)
		if pricing != nil && pricing.Unit == "" {
			pricing.Unit = registry.PricingUnit
			pricing.VersionID = pricingtypes.VersionID(pricing)
		}
		data, _ := json.Marshal(pricing)
		count += len(capability.Patterns)
		size += (len(data) + 64) * len(capability.Patterns)
		if count > 4096 || size > 4<<20 {
			return nil, errors.New("price review model/byte limit exceeded")
		}
		for _, pattern := range capability.Patterns {
			if _, exists := out[pattern]; exists {
				return nil, errors.New("duplicate model pricing pattern")
			}
			out[pattern] = pricing
		}
	}
	return out, nil
}
func pricingVersion(bundle *PresetBundle) (string, error) {
	values, err := pricingMap(bundle)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(values)
	return "prices_" + sha256Hex(data), nil
}
func bundleHash(bundle *PresetBundle) string { data, _ := json.Marshal(bundle); return sha256Hex(data) }
func publicPriceSource(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.Fragment = ""
	q := parsed.Query()
	for key := range q {
		q.Set(key, "[REDACTED]")
	}
	parsed.RawQuery = q.Encode()
	return parsed.String()
}
func priceAnomaly(before, after *pricingtypes.Pricing, threshold float64) []string {
	if before == nil || after == nil {
		return []string{"price_added_or_removed"}
	}
	if !strings.EqualFold(before.Currency, after.Currency) || before.Unit != after.Unit {
		return []string{"unit_or_currency_changed"}
	}
	// Compare every numeric leaf, including tier thresholds and image dimensions.
	var a, b map[string]any
	raw, _ := json.Marshal(before)
	_ = json.Unmarshal(raw, &a)
	raw, _ = json.Marshal(after)
	_ = json.Unmarshal(raw, &b)
	delete(a, "versionId")
	delete(b, "versionId")
	var walk func(any, any) bool
	walk = func(old, next any) bool {
		switch x := old.(type) {
		case float64:
			y, ok := next.(float64)
			if !ok {
				return true
			}
			if x == 0 {
				return y != 0
			}
			return math.Abs(y-x)/math.Abs(x)*100 > threshold
		case map[string]any:
			y, ok := next.(map[string]any)
			if !ok {
				return true
			}
			if len(x) != len(y) {
				return true
			}
			for key, value := range x {
				if walk(value, y[key]) {
					return true
				}
			}
			return false
		case []any:
			y, ok := next.([]any)
			if !ok || len(x) != len(y) {
				return true
			}
			for i, value := range x {
				if walk(value, y[i]) {
					return true
				}
			}
			return false
		default:
			return fmt.Sprint(old) != fmt.Sprint(next)
		}
	}
	if walk(a, b) {
		return []string{"price_jump_or_dimension_change"}
	}
	return nil
}
func (u *PresetUpdater) stagePriceReview(bundle *PresetBundle, index *PresetIndex, indexBytes []byte, verified bool, sourceKind ...string) (bool, error) {
	current := u.store.Get()
	before, err := pricingVersion(current)
	if err != nil {
		return false, err
	}
	after, err := pricingVersion(bundle)
	if err != nil {
		return false, err
	}
	if before == after {
		state := u.priceDiskSnapshot()
		if state.Pending != nil && compareDataVersion(bundle.DataVersion, state.Pending.Proposal.DataVersion) >= 0 {
			state.Pending = nil
			if err := u.savePriceDisk(state); err != nil {
				return false, err
			}
			u.setPriceDisk(state)
		}
		return false, nil
	}
	oldPrices, _ := pricingMap(current)
	newPrices, _ := pricingMap(bundle)
	models := map[string]bool{}
	for name := range oldPrices {
		models[name] = true
	}
	for name := range newPrices {
		models[name] = true
	}
	names := make([]string, 0, len(models))
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)
	threshold := u.config.PriceChangeThresholdPercent
	if threshold <= 0 {
		threshold = 50
	}
	var published *time.Time
	if !index.PublishedAt.IsZero() {
		value := index.PublishedAt
		published = &value
	}
	kind := "remote"
	if len(sourceKind) > 0 && sourceKind[0] == "legacy_cache" {
		kind = "legacy_cache"
	}
	proposal := PriceProposal{SourceKind: kind, SourcePublishedAt: published, SourceCheckedAt: time.Now().UTC(), BeforeBundleHash: bundleHash(current), BeforeVersion: before, AfterVersion: after, DataVersion: bundle.DataVersion, SourceURL: publicPriceSource(u.config.IndexURL), SourceDigest: sha256Hex(indexBytes), SigningKeyID: index.SigningKeyID, SignatureVerified: verified, CreatedAt: time.Now().UTC(), ThresholdPercent: threshold, Changes: []PriceChange{}}
	if kind == "legacy_cache" {
		proposal.SourceURL = ""
	}
	for _, name := range names {
		a, oldExists := oldPrices[name]
		b, newExists := newPrices[name]
		if oldExists == newExists && pricingtypes.VersionID(a) == pricingtypes.VersionID(b) {
			continue
		}
		proposal.ChangeCount++
		reasons := priceAnomaly(a, b, threshold)
		if a == nil && b == nil && oldExists != newExists {
			reasons = []string{"pricing_pattern_shadowing_change"}
		}
		if len(reasons) > 0 {
			proposal.RequiresAnomalyAcknowledgement = true
		}

		if len(proposal.Changes) < 500 {
			proposal.Changes = append(proposal.Changes, PriceChange{Model: name, Before: a, After: b, Anomaly: len(reasons) > 0, Reasons: reasons})
		}
	}
	// Nil-price patterns also participate: exact/longer matches can shadow a
	// priced wildcard even though no numeric price leaf changed.
	if proposal.ChangeCount == 0 {
		return false, nil
	}
	proposal.PreviewTruncated = proposal.ChangeCount > len(proposal.Changes)
	if proposal.PreviewTruncated {
		proposal.RequiresAnomalyAcknowledgement = true
	}
	proposal.ReviewID = "review_" + sha256Hex([]byte(proposal.BeforeBundleHash+before+bundleHash(bundle)+proposal.SourceDigest))
	state := u.priceDiskSnapshot()
	if state.Pending != nil && state.Pending.Proposal.ReviewID == proposal.ReviewID {
		return true, nil
	}
	copyIndex := *index
	copyIndex.Shards = append([]PresetIndexShard(nil), index.Shards...)
	state.Pending = &pricePending{SourceIndex: &copyIndex, Proposal: proposal, Bundle: cloneBundle(bundle)}
	if err := u.savePriceDisk(state); err != nil {
		return false, err
	}
	u.setPriceDisk(state)
	log.Printf("[PresetUpdater-PriceReview] staged review=%s changes=%d anomalies=%t", proposal.ReviewID, proposal.ChangeCount, proposal.RequiresAnomalyAcknowledgement)
	return true, nil
}
func (u *PresetUpdater) priceDiskSnapshot() priceReviewDisk {
	u.priceMu.RLock()
	defer u.priceMu.RUnlock()
	data, _ := json.Marshal(u.priceState)
	var state priceReviewDisk
	_ = json.Unmarshal(data, &state)
	return state
}
func (u *PresetUpdater) setPriceDisk(state priceReviewDisk) {
	u.priceMu.Lock()
	defer u.priceMu.Unlock()
	if state.SchemaVersion == 0 {
		state.SchemaVersion = 1
	}
	u.priceState = state
}
func (u *PresetUpdater) savePriceDisk(state priceReviewDisk) error {
	if strings.TrimSpace(u.config.CacheDir) == "" {
		return errors.New("price changes require a durable preset cache directory")
	}
	state.SchemaVersion = 1
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(data) > maxPriceReviewBytes {
		return errors.New("price review persistence byte limit exceeded")
	}
	if err := os.MkdirAll(u.config.CacheDir, 0700); err != nil {
		return err
	}
	return atomicFileWriter(filepath.Join(u.config.CacheDir, priceReviewFile), data, 0600)
}
func (u *PresetUpdater) loadPriceDisk() error {
	path := filepath.Join(u.config.CacheDir, priceReviewFile)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maxPriceReviewBytes {
		return errors.New("invalid price review file")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("price review file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPriceReviewBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxPriceReviewBytes {
		return errors.New("price review file size limit exceeded")
	}
	var state priceReviewDisk
	if json.Unmarshal(data, &state) != nil {
		return errors.New("invalid price review cache")
	}
	if state.SchemaVersion != 1 {
		return errors.New("unsupported price review cache version")
	}
	if len(state.History) > 10 || len(state.Audit) > 1000 {
		return errors.New("price review history limit exceeded")
	}
	if state.CachedSource != nil {
		receipt := state.CachedSource
		if receipt.SourceIndex == nil || len(receipt.BundleHash) != 64 || receipt.Revision.BundleHash != receipt.BundleHash || receipt.Revision.VersionID != receipt.PricingVersion {
			return errors.New("invalid automatic source receipt")
		}
	}
	if state.Pending != nil {
		if state.Pending.Bundle == nil || state.Pending.SourceIndex == nil || state.Pending.SourceIndex.DataVersion != state.Pending.Bundle.DataVersion {
			return errors.New("pending price source version mismatch")
		}
		if len(state.Pending.Proposal.Changes) > 500 || state.Pending.Proposal.ChangeCount > 4096 {
			return errors.New("price proposal limit exceeded")
		}
		if Validate(state.Pending.Bundle) != nil || bundleHash(state.Pending.Bundle) == "" {
			return errors.New("invalid pending price bundle")
		}
		version, err := pricingVersion(state.Pending.Bundle)
		if err != nil || version != state.Pending.Proposal.AfterVersion {
			return errors.New("pending pricing fingerprint mismatch")
		}
	}
	for _, history := range state.History {
		version, versionErr := pricingVersion(history.Bundle)
		if versionErr != nil || version != history.Revision.VersionID {
			return errors.New("price history version mismatch")
		}
		if Validate(history.Bundle) != nil || bundleHash(history.Bundle) != history.Revision.BundleHash {
			return errors.New("price history fingerprint mismatch")
		}
	}
	if state.ApprovedBundleHash != "" {
		found := false
		for _, entry := range state.History {
			if entry.Revision.BundleHash == state.ApprovedBundleHash {
				found = true
			}
		}
		if !found {
			return errors.New("approved pricing snapshot missing from history")
		}
	}
	u.setPriceDisk(state)
	return nil
}
func (u *PresetUpdater) PriceReviewState() PriceReviewState {
	u.checkMu.Lock()
	defer u.checkMu.Unlock()
	state := u.priceDiskSnapshot()
	current := u.store.Get()
	version, _ := pricingVersion(current)
	status := u.Status()
	lastError := ""
	if status.LastError != "" {
		lastError = "source_check_failed"
	}
	result := PriceReviewState{RollbackBlockedThrough: state.RollbackBlockedThrough, LastCheckError: lastError, LastCheckAt: status.LastCheckAt, SignatureRequired: len(u.config.TrustedSigningKeys) > 0, SourceMaxAge: u.config.SourceMaxAge.String(), SourceFutureSkew: u.config.SourceFutureSkew.String(), ActiveVersion: version, ActiveDataVersion: current.DataVersion, ActiveBundleHash: bundleHash(current), History: []PriceRevision{}, Audit: append([]PriceAudit{}, state.Audit...), RollbackScope: "entire preset bundle, including model capabilities; existing settled prices are not recalculated"}
	if state.Pending != nil {
		proposal := state.Pending.Proposal
		_, policyErr := u.verifyIndexSignature(state.Pending.SourceIndex)
		if policyErr == nil {
			policyErr = u.validateSourceFreshness(state.Pending.SourceIndex, time.Now())
		}
		if policyErr != nil {
			proposal.PolicyError = "source_policy_rejected"
		}
		result.Pending = &proposal
	}
	for _, history := range state.History {
		result.History = append(result.History, history.Revision)
		if history.Revision.VersionID == version && history.Revision.SourceDigest != "" {
			source := history.Revision
			result.ActivePriceSource = &source
		}
	}
	if state.CachedSource != nil && state.CachedSource.BundleHash == result.ActiveBundleHash {
		source := state.CachedSource.Revision
		result.ActivePriceSource = &source
	}
	return result
}
func appendPriceHistory(state *priceReviewDisk, bundle *PresetBundle, actor string) error {
	hash := bundleHash(bundle)
	for i, entry := range state.History {
		if entry.Revision.BundleHash == hash {
			state.History = append(append(state.History[:i:i], state.History[i+1:]...), entry)
			return nil
		}
	}
	version, err := pricingVersion(bundle)
	if err != nil {
		return err
	}
	state.History = append(state.History, priceHistory{Revision: PriceRevision{VersionID: version, DataVersion: bundle.DataVersion, BundleHash: hash, At: time.Now().UTC(), ActorID: actor}, Bundle: cloneBundle(bundle)})
	for {
		data, _ := json.Marshal(state.History)
		if len(state.History) <= 10 && len(data) <= maxPriceHistoryBytes {
			break
		}
		if len(state.History) <= 1 {
			return errors.New("price history byte limit exceeded")
		}
		state.History = state.History[1:]
	}
	return nil
}
func (u *PresetUpdater) activateReviewedBundle(state priceReviewDisk, bundle *PresetBundle, actor, action, digest string) error {
	if strings.TrimSpace(actor) == "" || len(actor) > 256 {
		return errors.New("price activation requires an audit actor")
	}
	if err := Validate(bundle); err != nil {
		return err
	}
	if err := appendPriceHistory(&state, u.store.Get(), actor); err != nil {
		return err
	}
	if err := appendPriceHistory(&state, bundle, actor); err != nil {
		return err
	}
	if action == "price_approval" && state.Pending != nil {
		for i := range state.History {
			if state.History[i].Revision.BundleHash == bundleHash(bundle) {
				state.History[i].Revision.SourcePublishedAt = state.Pending.Proposal.SourcePublishedAt
				checked := state.Pending.Proposal.SourceCheckedAt
				state.History[i].Revision.SourceCheckedAt = &checked
				state.History[i].Revision.SourceDigest = state.Pending.Proposal.SourceDigest
				state.History[i].Revision.SigningKeyID = state.Pending.Proposal.SigningKeyID
				state.History[i].Revision.SignatureVerified = state.Pending.Proposal.SignatureVerified
			}
		}
	}
	version, err := pricingVersion(bundle)
	if err != nil {
		return err
	}
	state.Audit = append(state.Audit, PriceAudit{At: time.Now().UTC(), ActorID: actor, Action: action, VersionID: version, SourceDigest: digest})
	if len(state.Audit) > 1000 {
		state.Audit = state.Audit[len(state.Audit)-1000:]
	}
	if action == "price_rollback" {
		version := u.store.DataVersion()
		if compareDataVersion(version, state.RollbackBlockedThrough) > 0 {
			state.RollbackBlockedThrough = version
		}
	} else {
		state.RollbackBlockedThrough = ""
	}
	state.CachedSource = nil
	state.ApprovedBundleHash = bundleHash(bundle)
	state.Pending = nil
	// A single fsynced/renamed review file contains the approved bundle, bounded
	// rollback history and actor audit. It is the authoritative price cache;
	// avoid a multi-file audit/cache transaction that could apply an unapproved
	// price or lose a successful rollback after a crash.
	if err := u.savePriceDisk(state); err != nil {
		return err
	}
	u.setPriceDisk(state)
	u.store.Swap(bundle)
	u.mu.Lock()
	u.source = action
	u.dataVersion = bundle.DataVersion
	u.cacheValid = true
	u.lastError = ""
	u.mu.Unlock()
	return nil
}
func (u *PresetUpdater) ApprovePriceReview(actor, reviewID string, allowAnomalies bool) error {
	u.checkMu.Lock()
	defer u.checkMu.Unlock()
	state := u.priceDiskSnapshot()
	if state.Pending == nil || state.Pending.Proposal.ReviewID != reviewID {
		return ErrPriceReviewConflict
	}
	current, _ := pricingVersion(u.store.Get())
	if current != state.Pending.Proposal.BeforeVersion || bundleHash(u.store.Get()) != state.Pending.Proposal.BeforeBundleHash {
		return ErrPriceReviewConflict
	}
	verified, sourceErr := u.verifyIndexSignature(state.Pending.SourceIndex)
	if sourceErr == nil {
		sourceErr = u.validateSourceFreshness(state.Pending.SourceIndex, time.Now())
	}
	if sourceErr != nil {
		return fmt.Errorf("%w: %v", ErrPriceReviewConflict, sourceErr)
	}
	state.Pending.Proposal.SignatureVerified = verified
	state.Pending.Proposal.SigningKeyID = state.Pending.SourceIndex.SigningKeyID
	if state.Pending.Proposal.RequiresAnomalyAcknowledgement && !allowAnomalies {
		return ErrPriceAnomalyAck
	}
	return u.activateReviewedBundle(state, state.Pending.Bundle, actor, "price_approval", state.Pending.Proposal.SourceDigest)
}
func (u *PresetUpdater) RollbackPriceVersion(actor, bundleHashID string) error {
	u.checkMu.Lock()
	defer u.checkMu.Unlock()
	state := u.priceDiskSnapshot()
	for _, entry := range state.History {
		if entry.Revision.BundleHash == bundleHashID {
			return u.activateReviewedBundle(state, entry.Bundle, actor, "price_rollback", entry.Revision.BundleHash)
		}
	}
	return ErrPriceRollbackUnavailable
}

// approvedPriceBundle is already validated by loadPriceDisk. Local audit grants
// survive a deliberate rollback to a lower dataVersion.
func (u *PresetUpdater) approvedPriceBundle() *PresetBundle {
	state := u.priceDiskSnapshot()
	if state.ApprovedBundleHash == "" {
		return nil
	}
	for _, entry := range state.History {
		if entry.Revision.BundleHash == state.ApprovedBundleHash {
			return cloneBundle(entry.Bundle)
		}
	}
	return nil
}

func (u *PresetUpdater) validateSourceFreshness(index *PresetIndex, now time.Time) error {
	if u.config.SourceMaxAge <= 0 && u.config.SourceFutureSkew <= 0 {
		return nil
	}
	if index == nil || index.PublishedAt.IsZero() {
		return errors.New("preset source publication time is required by freshness policy")
	}
	if index.PublishedAt.After(now.Add(u.config.SourceFutureSkew)) {
		return errors.New("preset source publication time is too far in the future")
	}
	if u.config.SourceMaxAge > 0 && now.Sub(index.PublishedAt) > u.config.SourceMaxAge {
		return errors.New("preset source publication time exceeds maximum age")
	}
	return nil
}

// recordAutomaticSource records that the exact cache passed the same pricing
// selection fingerprint gate. It is not a human price approval and cannot
// supersede an explicit rollback once that rollback clears this receipt.
func (u *PresetUpdater) recordAutomaticSource(bundle *PresetBundle, index *PresetIndex, indexBytes []byte, verified bool) error {
	if strings.TrimSpace(u.config.CacheDir) == "" {
		return nil
	}
	version, err := pricingVersion(bundle)
	if err != nil {
		return err
	}
	state := u.priceDiskSnapshot()
	copy := *index
	copy.Shards = append([]PresetIndexShard(nil), index.Shards...)
	var published *time.Time
	if !index.PublishedAt.IsZero() {
		value := index.PublishedAt
		published = &value
	}
	checked := time.Now().UTC()
	state.CachedSource = &presetCacheReceipt{BundleHash: bundleHash(bundle), PricingVersion: version, SourceIndex: &copy, Revision: PriceRevision{VersionID: version, DataVersion: bundle.DataVersion, BundleHash: bundleHash(bundle), At: checked, ActorID: "system:preset-updater", SourceDigest: sha256Hex(indexBytes), SigningKeyID: index.SigningKeyID, SignatureVerified: verified, SourcePublishedAt: published, SourceCheckedAt: &checked}}
	state.RollbackBlockedThrough = ""
	if err := u.savePriceDisk(state); err != nil {
		return err
	}
	u.setPriceDisk(state)
	return nil
}
func (u *PresetUpdater) validAutomaticCache(bundle *PresetBundle, state priceReviewDisk) bool {
	receipt := state.CachedSource
	if receipt == nil || receipt.SourceIndex == nil || receipt.BundleHash != bundleHash(bundle) || receipt.SourceIndex.DataVersion != bundle.DataVersion {
		return false
	}
	version, err := pricingVersion(bundle)
	if err != nil || version != receipt.PricingVersion {
		return false
	}
	if _, err := u.verifyIndexSignature(receipt.SourceIndex); err != nil {
		return false
	}
	if state.RollbackBlockedThrough != "" && compareDataVersion(bundle.DataVersion, state.RollbackBlockedThrough) <= 0 {
		return false
	}
	baseline := u.store.Get()
	if compareDataVersion(bundle.DataVersion, baseline.DataVersion) <= 0 {
		return false
	}
	if approved := u.approvedPriceBundle(); approved != nil {
		baseline = approved
	}
	baselineVersion, err := pricingVersion(baseline)
	if err != nil || baselineVersion != version {
		return false
	}

	return true
}
