package statebundle

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (Options, string) {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, "settings")
	state := filepath.Join(root, "state")
	for _, d := range []string{configDir, state, filepath.Join(state, "presets")} {
		if e := os.MkdirAll(d, 0700); e != nil {
			t.Fatal(e)
		}
	}
	cfg := filepath.Join(configDir, "config.json")
	if e := os.WriteFile(cfg, []byte(`{"identityProxy":{"revokedSessions":[{"sessionFingerprint":"revoked"}]}}`), 0600); e != nil {
		t.Fatal(e)
	}
	for name, body := range map[string]string{"presets/price-review.json": `{"activeVersion":"v1","audit":["approved"]}`, "conversation_state.json": `{"session":"route-a"}`} {
		if e := os.WriteFile(filepath.Join(state, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	secrets := filepath.Join(root, "secrets.env")
	if e := os.WriteFile(secrets, []byte("ADMIN_ACCESS_KEY=test-only-secret\n"), 0600); e != nil {
		t.Fatal(e)
	}
	return Options{ConfigPath: cfg, StateDir: state, SecretsPath: secrets}, root
}
func TestColdBundleRestoresWALBudgetAndSecurityState(t *testing.T) {
	opts, root := fixture(t)
	db, e := sql.Open("sqlite", filepath.Join(filepath.Dir(opts.ConfigPath), "entry-budget.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA wal_autocheckpoint=0", "CREATE TABLE budget(spent INTEGER)", "INSERT INTO budget VALUES(12345)"} {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	bundle := filepath.Join(root, "bundle")
	if e = Backup(opts, bundle); e != nil {
		t.Fatal(e)
	}
	m, e := Verify(bundle)
	if e != nil {
		t.Fatal(e)
	}
	if len(m.Files) != 5 {
		t.Fatalf("files=%v", m.Files)
	}
	dest := filepath.Join(root, "restored")
	if e = Restore(bundle, dest); e != nil {
		t.Fatal(e)
	}
	restored, e := openDB(filepath.Join(dest, "config", "entry-budget.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	var spent int
	if e = restored.QueryRow("SELECT spent FROM budget").Scan(&spent); e != nil || spent != 12345 {
		t.Fatalf("spent=%d err=%v", spent, e)
	}
	for _, p := range []string{"config/config.json", "state/presets/price-review.json", "secrets.env"} {
		info, e := os.Stat(filepath.Join(dest, p))
		if e != nil {
			t.Fatal(e)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatal("insecure recovered permissions")
		}
	}
	cfg, _ := os.ReadFile(filepath.Join(dest, "config/config.json"))
	if !strings.Contains(string(cfg), "revoked") {
		t.Fatal("identity revocations lost")
	}
	if e = Restore(bundle, dest); e == nil {
		t.Fatal("overwrote existing destination")
	}
}
func TestBusyBackupNeverTouchesSourceOrDestination(t *testing.T) {
	opts, root := fixture(t)
	l, e := Acquire(opts.ConfigPath, opts.StateDir)
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	output := filepath.Join(root, "bundle")
	if e = Backup(opts, output); !errors.Is(e, ErrBusy) {
		t.Fatalf("err=%v", e)
	}
	if _, e = os.Stat(output); !os.IsNotExist(e) {
		t.Fatal("busy backup created output")
	}
}
func TestBundleRejectsCorruptionTraversalAndUnlistedFiles(t *testing.T) {
	for _, kind := range []string{"corruption", "traversal", "unlisted", "symlink", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			opts, root := fixture(t)
			bundle := filepath.Join(root, "bundle")
			if e := Backup(opts, bundle); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "corruption":
				_ = os.WriteFile(filepath.Join(bundle, "config/config.json"), []byte("changed"), 0600)
			case "unlisted":
				_ = os.WriteFile(filepath.Join(bundle, "extra"), []byte("data"), 0600)
			case "symlink":
				_ = os.Remove(filepath.Join(bundle, "config/config.json"))
				if e := os.Symlink(opts.ConfigPath, filepath.Join(bundle, "config/config.json")); e != nil {
					t.Skip(e)
				}
			default:
				raw, _ := os.ReadFile(filepath.Join(bundle, "manifest.json"))
				var m Manifest
				_ = json.Unmarshal(raw, &m)
				if kind == "traversal" {
					m.Files[0].Path = "state/../../outside"
				} else {
					m.Files = append(m.Files, m.Files[0])
				}
				raw, _ = json.Marshal(m)
				_ = os.WriteFile(filepath.Join(bundle, "manifest.json"), raw, 0600)
			}
			output := filepath.Join(root, "recovered")
			if e := Restore(bundle, output); e == nil {
				t.Fatal("unsafe bundle accepted")
			}
			if _, e := os.Stat(output); !os.IsNotExist(e) {
				t.Fatal("failed verification created output")
			}
		})
	}
}
func TestBundleRejectsNestedOutputAndIncompleteState(t *testing.T) {
	opts, _ := fixture(t)
	if e := Backup(opts, filepath.Join(opts.StateDir, "bundle")); e == nil {
		t.Fatal("recursive output accepted")
	}
	if e := os.WriteFile(filepath.Join(opts.StateDir, ".bundle-incomplete"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Acquire(opts.ConfigPath, opts.StateDir); e == nil {
		t.Fatal("incomplete restore opened")
	}
}

func TestBundleRejectsSymlinkRootAndCorruptDatabase(t *testing.T) {
	opts, root := fixture(t)
	alias := filepath.Join(root, "state-alias")
	if err := os.Symlink(opts.StateDir, alias); err != nil {
		t.Skip(err)
	}
	bad := opts
	bad.StateDir = alias
	if err := Backup(bad, filepath.Join(root, "bad-bundle")); err == nil {
		t.Fatal("symlink state silently omitted")
	}
	if err := os.WriteFile(filepath.Join(opts.StateDir, "metrics.db"), []byte("broken database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Backup(opts, filepath.Join(root, "corrupt-bundle")); err == nil {
		t.Fatal("corrupt database accepted as a regular file")
	}
	if _, err := os.Stat(filepath.Join(root, "corrupt-bundle")); !os.IsNotExist(err) {
		t.Fatal("failed backup published")
	}
}

func TestOutputAliasesCannotChangeSnapshotSources(t *testing.T) {
	opts, root := fixture(t)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(opts.StateDir, alias); err != nil {
		t.Skip(err)
	}
	if err := Backup(opts, filepath.Join(alias, "bundle")); err == nil {
		t.Fatal("backup output alias entered source")
	}
	bundle := filepath.Join(root, "bundle")
	if err := Backup(opts, bundle); err != nil {
		t.Fatal(err)
	}
	if err := Restore(bundle, filepath.Join(bundle, "restored")); err == nil {
		t.Fatal("restore contaminated its source bundle")
	}
	if _, err := Verify(bundle); err != nil {
		t.Fatalf("source bundle changed: %v", err)
	}
}

func TestRelativePathsBackupVerifyRestoreSQLite(t *testing.T) {
	opts, root := fixture(t)
	db, err := sql.Open("sqlite", filepath.Join(opts.StateDir, "metrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE values_saved(value INTEGER); INSERT INTO values_saved VALUES(42)"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	t.Chdir(root)
	opts.ConfigPath = filepath.Join("settings", "config.json")
	opts.StateDir = "state"
	opts.SecretsPath = "secrets.env"
	if err = Backup(opts, "bundle"); err != nil {
		t.Fatal(err)
	}
	if _, err = Verify("bundle"); err != nil {
		t.Fatal(err)
	}
	if err = Restore("bundle", "restored"); err != nil {
		t.Fatal(err)
	}
	recovered, err := openDB(filepath.Join("restored", "state", "metrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	var value int
	if err = recovered.QueryRow("SELECT value FROM values_saved").Scan(&value); err != nil || value != 42 {
		t.Fatalf("value=%d err=%v", value, err)
	}
}
func TestAcquireDeduplicatesDirectoryAliases(t *testing.T) {
	opts, root := fixture(t)
	alias := filepath.Join(root, "config-alias")
	if err := os.Symlink(opts.StateDir, alias); err != nil {
		t.Skip(err)
	}
	l, err := Acquire(filepath.Join(alias, "config.json"), opts.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.files) != 1 {
		t.Fatal("same directory locked twice")
	}
	if _, err = Acquire(filepath.Join(alias, "config.json"), opts.StateDir); !errors.Is(err, ErrBusy) {
		t.Fatalf("other instance admitted: %v", err)
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Acquire(filepath.Join(alias, "config.json"), opts.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
}
