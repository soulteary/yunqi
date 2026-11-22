package presetstore

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pricedTestBundle(version string, price float64) *PresetBundle {
	b := validBundle()
	b.DataVersion = version
	b.ModelRegistry = validModelRegistryPreset()
	b.ModelRegistry.UpstreamCapabilities[0].Pricing = &ModelPricingPreset{Currency: "USD", InputCacheMissPrice: floatPtr(price)}
	b.ChannelPresets = validChannelPresetsPreset()
	b.BuiltinModelsManifests = validBuiltinManifestPreset()
	return b
}
func TestPriceUpdaterStagesApprovalAndDurableWholeBundleRollback(t *testing.T) {
	before := pricedTestBundle("2026.07.10-1", 1)
	incoming := pricedTestBundle("2026.07.10-2", 3)
	server := newPresetTestServer(t, incoming, presetTestServerOptions{})
	defer server.Close()
	cache := t.TempDir()
	store := NewPresetStore(before)
	updater := NewPresetUpdater(store, UpdaterConfig{IndexURL: server.URL + "/index.json?token=do-not-expose#fragment-secret", CacheDir: cache, AllowInsecureForTesting: true})
	if err := updater.CheckOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := updater.PriceReviewState()
	if state.Pending == nil || state.Pending.ChangeCount != 1 || !state.Pending.RequiresAnomalyAcknowledgement || state.Pending.SignatureVerified || strings.Contains(state.Pending.SourceURL, "do-not-expose") || strings.Contains(state.Pending.SourceURL, "fragment-secret") {
		t.Fatalf("invalid proposal: %+v", state)
	}
	if store.DataVersion() != before.DataVersion {
		t.Fatal("unapproved price silently activated")
	}
	review := state.Pending.ReviewID
	if err := updater.ApprovePriceReview("owner", review, false); !errors.Is(err, ErrPriceAnomalyAck) {
		t.Fatalf("jump was not held for acknowledgement: %v", err)
	}
	if err := updater.ApprovePriceReview("owner", "stale", true); !errors.Is(err, ErrPriceReviewConflict) {
		t.Fatal("stale approval accepted")
	}
	if err := updater.ApprovePriceReview("owner", review, true); err != nil {
		t.Fatal(err)
	}
	approved := updater.PriceReviewState()
	if approved.Pending != nil || store.DataVersion() != incoming.DataVersion || len(approved.History) != 2 || len(approved.Audit) != 1 || approved.Audit[0].ActorID != "owner" {
		t.Fatalf("approval state: %+v", approved)
	}
	info, err := os.Stat(filepath.Join(cache, priceReviewFile))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("audit file mode=%v err=%v", info, err)
	}
	restarted := NewPresetUpdater(NewPresetStore(pricedTestBundle("2026.07.10-9", 99)), UpdaterConfig{CacheDir: cache})
	if err := restarted.LoadCacheAtStartup(); err != nil || restarted.store.DataVersion() != incoming.DataVersion {
		t.Fatalf("approved bundle lost on restart: %v", err)
	}
	if err := updater.RollbackPriceVersion("rollback-owner", approved.History[0].BundleHash); err != nil {
		t.Fatal(err)
	}
	if store.DataVersion() != before.DataVersion {
		t.Fatal("explicit bundle rollback did not restore prior source")
	}
	// A newer embedded baseline must not undo an explicitly approved rollback.
	restartRollback := NewPresetUpdater(NewPresetStore(pricedTestBundle("2026.07.10-9", 99)), UpdaterConfig{CacheDir: cache})
	if err := restartRollback.LoadCacheAtStartup(); err != nil || restartRollback.store.DataVersion() != before.DataVersion {
		t.Fatalf("rollback lost on restart: %v", err)
	}
}
func TestPriceAuditFailureCannotActivateAndProposalIsNotAliased(t *testing.T) {
	before := pricedTestBundle("2026.07.10-1", 1)
	next := pricedTestBundle("2026.07.10-2", 1.1)
	updater := NewPresetUpdater(NewPresetStore(before), UpdaterConfig{CacheDir: t.TempDir(), IndexURL: "https://example.test/index.json"})
	staged, err := updater.stagePriceReview(next, &PresetIndex{DataVersion: next.DataVersion}, []byte("test-source"), false)
	if err != nil || !staged {
		t.Fatal(err)
	}
	proposal := updater.PriceReviewState().Pending
	*proposal.Changes[0].After.InputCacheMissPrice = 999
	if *updater.PriceReviewState().Pending.Changes[0].After.InputCacheMissPrice == 999 {
		t.Fatal("public proposal aliases stored prices")
	}
	oldWriter := atomicFileWriter
	atomicFileWriter = func(string, []byte, os.FileMode) error { return errors.New("disk write refused") }
	defer func() { atomicFileWriter = oldWriter }()
	if err := updater.ApprovePriceReview("owner", proposal.ReviewID, true); err == nil {
		t.Fatal("audit failure accepted")
	}
	if updater.store.DataVersion() != before.DataVersion || len(updater.PriceReviewState().Audit) != 0 {
		t.Fatal("failed durable approval changed runtime or audit")
	}
}
func TestPriceProposalRejectsStaleCapabilitySnapshotAndCorruptHistory(t *testing.T) {
	before := pricedTestBundle("2026.07.10-1", 1)
	next := pricedTestBundle("2026.07.10-2", 2)
	updater := NewPresetUpdater(NewPresetStore(before), UpdaterConfig{CacheDir: t.TempDir()})
	_, err := updater.stagePriceReview(next, &PresetIndex{DataVersion: next.DataVersion}, []byte("source"), false)
	if err != nil {
		t.Fatal(err)
	}
	review := updater.PriceReviewState().Pending.ReviewID
	changed := cloneBundle(before)
	changed.ModelRegistry.UpstreamCapabilities[0].ContextWindowTokens = 200000
	updater.store.Swap(changed)
	if err := updater.ApprovePriceReview("owner", review, true); !errors.Is(err, ErrPriceReviewConflict) {
		t.Fatal("stale capability snapshot overwrote current data")
	}
	if err := os.WriteFile(filepath.Join(updater.config.CacheDir, priceReviewFile), []byte(`{"history":[{"revision":{"bundleHash":"forged"}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	fresh := NewPresetUpdater(NewPresetStore(before), UpdaterConfig{CacheDir: updater.config.CacheDir})
	if err := fresh.LoadCacheAtStartup(); err == nil {
		t.Fatal("corrupt price history accepted")
	}
}

func TestPriceHistoryAndAuditAreBoundedAndOldRollbackCannotActivate(t *testing.T) {
	updater := NewPresetUpdater(NewPresetStore(pricedTestBundle("0", 1)), UpdaterConfig{CacheDir: t.TempDir()})
	original := updater.PriceReviewState().ActiveBundleHash
	for i := 0; i < 12; i++ {
		next := pricedTestBundle(fmt.Sprintf("version-%d", i+1), float64(i+2))
		if staged, err := updater.stagePriceReview(next, &PresetIndex{DataVersion: next.DataVersion}, []byte(fmt.Sprint(i)), false); err != nil || !staged {
			t.Fatal(err)
		}
		review := updater.PriceReviewState().Pending.ReviewID
		if i == 11 {
			state := updater.priceDiskSnapshot()
			state.Audit = make([]PriceAudit, 1000)
			for j := range state.Audit {
				state.Audit[j] = PriceAudit{ActorID: "prior-owner", Action: "prior-approval", VersionID: "prior-version"}
			}
			updater.setPriceDisk(state)
		}
		if err := updater.ApprovePriceReview("owner", review, true); err != nil {
			t.Fatal(err)
		}
	}
	state := updater.PriceReviewState()
	if len(state.History) > 10 || len(state.Audit) != 1000 || state.Audit[999].ActorID != "owner" {
		t.Fatalf("unbounded retained state: history=%d audit=%d", len(state.History), len(state.Audit))
	}
	active := state.ActiveBundleHash
	if err := updater.RollbackPriceVersion("owner", original); !errors.Is(err, ErrPriceRollbackUnavailable) {
		t.Fatal("evicted rollback snapshot accepted")
	}
	if updater.PriceReviewState().ActiveBundleHash != active {
		t.Fatal("unknown rollback changed runtime")
	}
}

func TestPriceSourceFreshnessPoliciesAndApprovalRecheck(t *testing.T) {
	now := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	updater := NewPresetUpdater(NewPresetStore(pricedTestBundle("0", 1)), UpdaterConfig{SourceMaxAge: 24 * time.Hour, SourceFutureSkew: 15 * time.Minute, CacheDir: t.TempDir()})
	for _, tc := range []struct {
		published time.Time
		valid     bool
	}{{time.Time{}, false}, {now.Add(-25 * time.Hour), false}, {now.Add(16 * time.Minute), false}, {now.Add(-time.Hour), true}, {now.Add(14 * time.Minute), true}} {
		err := updater.validateSourceFreshness(&PresetIndex{PublishedAt: tc.published}, now)
		if (err == nil) != tc.valid {
			t.Fatalf("published=%v policy error=%v", tc.published, err)
		}
	}
	compatible := NewPresetUpdater(nil, UpdaterConfig{})
	if err := compatible.validateSourceFreshness(&PresetIndex{}, now); err != nil {
		t.Fatal("disabled freshness policy rejected legacy missing timestamp")
	}
	next := pricedTestBundle("1", 2)
	index := &PresetIndex{DataVersion: next.DataVersion, PublishedAt: time.Now().Add(-48 * time.Hour)}
	if _, err := updater.stagePriceReview(next, index, []byte("source"), false); err != nil {
		t.Fatal(err)
	}
	review := updater.PriceReviewState().Pending.ReviewID
	if err := updater.ApprovePriceReview("owner", review, true); !errors.Is(err, ErrPriceReviewConflict) {
		t.Fatalf("expired persisted proposal approved: %v", err)
	}
	if updater.store.DataVersion() != "0" {
		t.Fatal("expired source changed prices")
	}
}

func TestPriceRollbackRestoresExactHashAfterSamePriceMetadataCache(t *testing.T) {
	base := pricedTestBundle("1", 1)
	b := pricedTestBundle("2", 2)
	cache := t.TempDir()
	updater := NewPresetUpdater(NewPresetStore(base), UpdaterConfig{CacheDir: cache})
	if _, err := updater.stagePriceReview(b, &PresetIndex{DataVersion: b.DataVersion}, []byte("b-source"), false); err != nil {
		t.Fatal(err)
	}
	if err := updater.ApprovePriceReview("owner", updater.PriceReviewState().Pending.ReviewID, true); err != nil {
		t.Fatal(err)
	}
	bHash := updater.PriceReviewState().ActiveBundleHash
	c := cloneBundle(b)
	c.DataVersion = "3"
	c.ModelRegistry.UpstreamCapabilities[0].ContextWindowTokens = 300000
	server := newPresetTestServer(t, c, presetTestServerOptions{})
	defer server.Close()
	updater.config.IndexURL = server.URL + "/index.json"
	updater.config.AllowInsecureForTesting = true
	updater.client = buildUpdaterHTTPClient(nil, redirectPolicy{allowInsecure: true})
	if err := updater.CheckOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if bundleHash(updater.store.Get()) == bHash {
		t.Fatal("same-price metadata update fixture did not apply")
	}
	if err := updater.RollbackPriceVersion("owner", bHash); err != nil {
		t.Fatal(err)
	}
	if err := updater.CheckOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if bundleHash(updater.store.Get()) != bHash {
		t.Fatal("rolled back source was immediately replayed")
	}
	restarted := NewPresetUpdater(NewPresetStore(base), UpdaterConfig{CacheDir: cache})
	if err := restarted.LoadCacheAtStartup(); err != nil {
		t.Fatal(err)
	}
	if bundleHash(restarted.store.Get()) != bHash {
		t.Fatal("newer same-price cache undid exact bundle rollback on restart")
	}
}
func TestLegacyUnapprovedCacheCannotActivateDifferentPricing(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		base := pricedTestBundle("1", 1)
		next := pricedTestBundle("2", 100)
		cache := t.TempDir()
		if err := SaveCache(cache, next); err != nil {
			t.Fatal(err)
		}
		cfg := UpdaterConfig{CacheDir: cache}
		if pinned {
			cfg.TrustedSigningKeys = map[string]ed25519.PublicKey{"test": make(ed25519.PublicKey, ed25519.PublicKeySize)}
		}
		updater := NewPresetUpdater(NewPresetStore(base), cfg)
		if err := updater.LoadCacheAtStartup(); err != nil {
			t.Fatal(err)
		}
		state := updater.PriceReviewState()
		if updater.store.DataVersion() != base.DataVersion || state.Pending == nil || state.Pending.SourceKind != "legacy_cache" {
			t.Fatal("unaudited cache changed active pricing")
		}
		if pinned {
			if err := updater.ApprovePriceReview("owner", state.Pending.ReviewID, true); !errors.Is(err, ErrPriceReviewConflict) {
				t.Fatal("pinned trust approved unsigned legacy cache")
			}
		}
	}
}
func TestEmptyModelRegistryUsesActualEmbeddedPricingFallback(t *testing.T) {
	embedded := EmbeddedBundle()
	fallback := cloneBundle(embedded)
	fallback.ModelRegistry = nil
	a, err := pricingVersion(embedded)
	if err != nil {
		t.Fatal(err)
	}
	b, err := pricingVersion(fallback)
	if err != nil || a != b {
		t.Fatal("fingerprint diverges from runtime embedded registry fallback")
	}
}

func TestAutomaticCacheReceiptCannotOverrideNewerOrDifferentBaseline(t *testing.T) {
	for _, tc := range []struct {
		baselineVersion string
		baselinePrice   float64
		pending         bool
	}{{"9", 9, true}, {"9", 1, false}, {"1", 9, true}} {
		cache := t.TempDir()
		a := pricedTestBundle("1", 1)
		b := pricedTestBundle("2", 1)
		b.ModelRegistry.UpstreamCapabilities[0].ContextWindowTokens = 200000
		source := newPresetTestServer(t, b, presetTestServerOptions{})
		updater := NewPresetUpdater(NewPresetStore(a), UpdaterConfig{CacheDir: cache, IndexURL: source.URL + "/index.json", AllowInsecureForTesting: true})
		if err := updater.CheckOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		source.Close()
		if updater.priceDiskSnapshot().ApprovedBundleHash != "" || updater.priceDiskSnapshot().CachedSource == nil {
			t.Fatal("fixture must be automatic, not an explicit approval")
		}
		baseline := pricedTestBundle(tc.baselineVersion, tc.baselinePrice)
		baseline.ModelRegistry.UpstreamCapabilities[0].ContextWindowTokens = 900000
		restarted := NewPresetUpdater(NewPresetStore(baseline), UpdaterConfig{CacheDir: cache})
		if err := restarted.LoadCacheAtStartup(); err != nil {
			t.Fatal(err)
		}
		state := restarted.PriceReviewState()
		if bundleHash(restarted.store.Get()) != bundleHash(baseline) || (state.Pending != nil) != tc.pending {
			t.Fatalf("automatic receipt overrode new baseline or lacked review: %+v", state)
		}
	}
}

func TestPriceChangeUnionLimitRoundTripsPendingAfterRestart(t *testing.T) {
	before := pricedTestBundle("1", 1)
	after := pricedTestBundle("2", 2)
	before.ModelRegistry.UpstreamCapabilities[0].Patterns = nil
	after.ModelRegistry.UpstreamCapabilities[0].Patterns = nil
	for i := 0; i < 2049; i++ {
		before.ModelRegistry.UpstreamCapabilities[0].Patterns = append(before.ModelRegistry.UpstreamCapabilities[0].Patterns, fmt.Sprintf("old-model-%04d", i))
		after.ModelRegistry.UpstreamCapabilities[0].Patterns = append(after.ModelRegistry.UpstreamCapabilities[0].Patterns, fmt.Sprintf("new-model-%04d", i))
	}
	updater := NewPresetUpdater(NewPresetStore(before), UpdaterConfig{CacheDir: t.TempDir()})
	staged, err := updater.stagePriceReview(after, &PresetIndex{DataVersion: after.DataVersion}, []byte("bulk-model-change"), false)
	if err != nil || !staged {
		t.Fatalf("stage failed: %v", err)
	}
	state := updater.PriceReviewState()
	if state.Pending == nil || state.Pending.ChangeCount != 4098 || !state.Pending.PreviewTruncated || len(state.Pending.Changes) != 500 {
		t.Fatal("source union or preview limit is incorrect")
	}
	restarted := NewPresetUpdater(NewPresetStore(before), UpdaterConfig{CacheDir: updater.config.CacheDir})
	if err := restarted.loadPriceDisk(); err != nil {
		t.Fatalf("a successfully staged proposal failed restart: %v", err)
	}
	restored := restarted.PriceReviewState()
	if restored.Pending == nil || restored.Pending.ChangeCount != 4098 || restored.Pending.ReviewID != state.Pending.ReviewID {
		t.Fatal("pending proposal changed across restart")
	}
	if err := restarted.ApprovePriceReview("owner", restored.Pending.ReviewID, true); err != nil {
		t.Fatalf("restored proposal cannot be approved: %v", err)
	}
	if bundleHash(restarted.store.Get()) != bundleHash(after) {
		t.Fatal("restored approval did not activate exact candidate")
	}
}

func TestPriceUpdaterRejectsOversizedIndexWithoutChangingActiveOrPending(t *testing.T) {
	before := pricedTestBundle("2026.07.10-1", 1)
	next := pricedTestBundle("2026.07.10-2", 3)
	oversized := []byte(`{"padding":"` + strings.Repeat("x", defaultIndexSizeLimit) + `"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(oversized) }))
	defer server.Close()
	store := NewPresetStore(before)
	updater := NewPresetUpdater(store, UpdaterConfig{IndexURL: server.URL + "/index.json", CacheDir: t.TempDir(), AllowInsecureForTesting: true})
	if _, err := updater.stagePriceReview(next, &PresetIndex{DataVersion: next.DataVersion}, []byte("prior-source"), false); err != nil {
		t.Fatal(err)
	}
	pendingBefore, _ := json.Marshal(updater.PriceReviewState().Pending)
	diskBefore, err := os.ReadFile(filepath.Join(updater.config.CacheDir, priceReviewFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := updater.CheckOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "1048576") {
		t.Fatalf("oversized index accepted: %v", err)
	}
	pendingAfter, _ := json.Marshal(updater.PriceReviewState().Pending)
	diskAfter, err := os.ReadFile(filepath.Join(updater.config.CacheDir, priceReviewFile))
	if err != nil {
		t.Fatal(err)
	}
	if store.DataVersion() != before.DataVersion || string(pendingBefore) != string(pendingAfter) || string(diskBefore) != string(diskAfter) {
		t.Fatal("oversized index changed active or pending review")
	}
}
