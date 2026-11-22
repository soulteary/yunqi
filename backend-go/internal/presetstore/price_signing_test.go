package presetstore

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestPresetPriceSignatureMatchesNodePublisher(t *testing.T) {
	data, err := os.ReadFile("testdata/signed-price-index.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		PublicKey string      `json:"publicKey"`
		Index     PresetIndex `json:"index"`
	}
	if json.Unmarshal(data, &fixture) != nil {
		t.Fatal("invalid fixture")
	}
	public, _ := base64.StdEncoding.DecodeString(fixture.PublicKey)
	updater := NewPresetUpdater(NewPresetStore(nil), UpdaterConfig{TrustedSigningKeys: map[string]ed25519.PublicKey{"test-release": public}})
	if verified, err := updater.verifyIndexSignature(&fixture.Index); err != nil || !verified {
		t.Fatalf("publisher/verifier mismatch: %v", err)
	}
	fixture.Index.Shards[0].URL = "https://tampered.test/changed"
	if _, err := updater.verifyIndexSignature(&fixture.Index); err == nil {
		t.Fatal("changed signed source accepted")
	}
}
func TestSignedPriceSourceStagesVerifiedAndRejectsTampering(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	pub := key.Public().(ed25519.PublicKey)
	before := pricedTestBundle("2026.07.10-1", 1)
	next := pricedTestBundle("2026.07.10-2", 2)
	for _, tamper := range []bool{false, true} {
		server := newPresetTestServer(t, next, presetTestServerOptions{signingKey: key, tamperSignature: tamper})
		updater := NewPresetUpdater(NewPresetStore(before), UpdaterConfig{IndexURL: server.URL + "/index.json", CacheDir: t.TempDir(), AllowInsecureForTesting: true, TrustedSigningKeys: map[string]ed25519.PublicKey{"test-release": pub}})
		err := updater.CheckOnce(context.Background())
		server.Close()
		state := updater.PriceReviewState()
		if tamper {
			if err == nil || state.Pending != nil {
				t.Fatal("tampered signed source was staged or applied")
			}
		} else {
			if err != nil || state.Pending == nil || !state.Pending.SignatureVerified {
				t.Fatalf("verified source provenance missing: %+v %v", state, err)
			}
		}
		if !tamper {
			review := updater.PriceReviewState().Pending.ReviewID
			if err := updater.ApprovePriceReview("signed-reviewer", review, true); err != nil {
				t.Fatal(err)
			}
			source := updater.PriceReviewState().ActivePriceSource
			if source == nil || !source.SignatureVerified || source.SigningKeyID != "test-release" || len(source.SourceDigest) != 64 {
				t.Fatal("verified source lost after durable approval")
			}
			continue
		}
		if updater.store.DataVersion() != before.DataVersion {
			t.Fatal("signature bypassed manual price approval")
		}
	}
}

func TestPinnedPriceSourceCannotStripSignatureOrReuseRevokedPendingKey(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	pub := key.Public().(ed25519.PublicKey)
	before := pricedTestBundle("0", 1)
	next := pricedTestBundle("1", 2)
	server := newPresetTestServer(t, next, presetTestServerOptions{})
	pinned := NewPresetUpdater(NewPresetStore(before), UpdaterConfig{IndexURL: server.URL + "/index.json", AllowInsecureForTesting: true, CacheDir: t.TempDir(), TrustedSigningKeys: map[string]ed25519.PublicKey{"test-release": pub}})
	if err := pinned.CheckOnce(context.Background()); err == nil {
		t.Fatal("stripping both signature and key ID bypassed pinned trust")
	}
	server.Close()
	signed := newPresetTestServer(t, next, presetTestServerOptions{signingKey: key})
	defer signed.Close()
	updater := NewPresetUpdater(NewPresetStore(before), UpdaterConfig{IndexURL: signed.URL + "/index.json", AllowInsecureForTesting: true, CacheDir: t.TempDir(), TrustedSigningKeys: map[string]ed25519.PublicKey{"test-release": pub}})
	if err := updater.CheckOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	review := updater.PriceReviewState().Pending.ReviewID
	// Simulate restart under a rotated public key with the same publisher ID.
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	restarted := NewPresetUpdater(NewPresetStore(before), UpdaterConfig{CacheDir: updater.config.CacheDir, TrustedSigningKeys: map[string]ed25519.PublicKey{"test-release": other}})
	if err := restarted.loadPriceDisk(); err != nil {
		t.Fatal(err)
	}
	if err := restarted.ApprovePriceReview("owner", review, true); !errors.Is(err, ErrPriceReviewConflict) {
		t.Fatal("persisted verified flag bypassed the current signing key")
	}
	if restarted.store.DataVersion() != before.DataVersion {
		t.Fatal("revoked source changed active price")
	}
}
