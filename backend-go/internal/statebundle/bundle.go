package statebundle

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const maxFiles = 10000
const maxBytes int64 = 16 << 30

type Entry struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	SQLite bool   `json:"sqlite,omitempty"`
}
type Manifest struct {
	Version          int       `json:"version"`
	CreatedAt        time.Time `json:"createdAt"`
	ExternalSnapshot string    `json:"externalSnapshot,omitempty"`
	Files            []Entry   `json:"files"`
}
type Options struct{ ConfigPath, StateDir, SecretsPath, ExternalSnapshot string }

// Backup is a cold snapshot: no cooperating service can write either source
// directory until every file and WAL-backed SQLite snapshot has been verified.
func Backup(opts Options, output string) (err error) {
	info, err := os.Lstat(opts.StateDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("state root must be a real directory")
	}
	l, err := Acquire(opts.ConfigPath, opts.StateDir)
	if err != nil {
		return err
	}
	defer l.Close()
	if err = checkOutput(opts, output); err != nil {
		return err
	}
	if err = os.Mkdir(output, 0700); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(output)
		}
	}()
	if err = os.WriteFile(filepath.Join(output, ".bundle-incomplete"), nil, 0600); err != nil {
		return err
	}
	m := Manifest{Version: 1, CreatedAt: time.Now().UTC(), ExternalSnapshot: opts.ExternalSnapshot}
	sources := map[string]string{"config/config.json": opts.ConfigPath}
	budget := filepath.Join(filepath.Dir(opts.ConfigPath), "entry-budget.db")
	if _, e := os.Lstat(budget); e == nil {
		sources["config/entry-budget.db"] = budget
	} else if !os.IsNotExist(e) {
		return e
	}
	if opts.SecretsPath != "" {
		sources["secrets.env"] = opts.SecretsPath
	}
	configAbs, _ := filepath.Abs(opts.ConfigPath)
	err = filepath.WalkDir(opts.StateDir, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if p == opts.StateDir {
			return nil
		}
		rel, e := filepath.Rel(opts.StateDir, p)
		if e != nil {
			return e
		}
		first := strings.Split(filepath.ToSlash(rel), "/")[0]
		if first == "backups" || first == "logs" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("state contains a symlink")
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("state contains a non-regular file")
		}
		if d.Name() == ".yunqi-state.lock" || strings.HasSuffix(p, "-wal") || strings.HasSuffix(p, "-shm") {
			return nil
		}
		a, _ := filepath.Abs(p)
		budgetAbs, _ := filepath.Abs(budget)
		if a == configAbs || a == budgetAbs {
			return nil
		}
		if len(sources) >= maxFiles {
			return errors.New("too many state files")
		}
		sources["state/"+filepath.ToSlash(rel)] = p
		return nil
	})
	if err != nil {
		return err
	}
	names := make([]string, 0, len(sources))
	for n := range sources {
		names = append(names, n)
	}
	sort.Strings(names)
	var total int64
	for _, n := range names {
		source := sources[n]
		info, e := os.Lstat(source)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return errors.New("source is not a regular file")
		}
		if info.Size() > maxBytes {
			return errors.New("state bundle exceeds size limit")
		}
		dest := filepath.Join(output, filepath.FromSlash(n))
		if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return e
		}
		sqlite, e := isSQLite(source)
		if e != nil {
			return e
		}
		if !sqlite && (strings.HasSuffix(source, ".db") || strings.HasSuffix(source, ".sqlite")) {
			return errors.New("invalid SQLite source")
		}
		if sqlite {
			e = copySQLite(source, dest)
		} else {
			e = copyFile(source, dest)
		}
		if e != nil {
			return e
		}
		entry, e := describe(dest, n, sqlite)
		if e != nil {
			return e
		}
		total += entry.Bytes
		if total > maxBytes {
			return errors.New("state bundle exceeds size limit")
		}
		m.Files = append(m.Files, entry)
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(output, "manifest.json"), raw, 0600); err != nil {
		return err
	}
	if err = syncFile(filepath.Join(output, "manifest.json")); err != nil {
		return err
	}
	if err = os.Remove(filepath.Join(output, ".bundle-incomplete")); err != nil {
		return err
	}
	return nil
}
func checkOutput(o Options, output string) error {
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil {
		return err
	}
	dest, err := filepath.Abs(filepath.Join(parent, filepath.Base(output)))
	if err != nil {
		return err
	}
	for _, s := range []string{o.StateDir, filepath.Dir(o.ConfigPath)} {
		real, e := filepath.EvalSymlinks(s)
		if e != nil {
			return e
		}
		src, e := filepath.Abs(real)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(src, dest)
		if e != nil {
			return e
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return errors.New("bundle must be outside source directories")
		}
	}
	return nil
}
func isSQLite(path string) (bool, error) {
	f, e := os.Open(path)
	if e != nil {
		return false, e
	}
	defer f.Close()
	b := make([]byte, 16)
	_, e = io.ReadFull(f, b)
	if errors.Is(e, io.EOF) || errors.Is(e, io.ErrUnexpectedEOF) {
		return false, nil
	}
	return string(b) == "SQLite format 3\x00", e
}
func openDB(path string) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: absolute}
	q := url.Values{"mode": {"ro"}, "_pragma": {"busy_timeout(5000)"}}
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e == nil {
		db.SetMaxOpenConns(1)
	}
	return db, e
}
func verifyDB(path string) error {
	db, e := openDB(path)
	if e != nil {
		return e
	}
	defer db.Close()
	var result string
	if e = db.QueryRow("PRAGMA integrity_check").Scan(&result); e != nil {
		return e
	}
	if result != "ok" {
		return errors.New("SQLite integrity check failed")
	}
	return nil
}
func copySQLite(source, dest string) error {
	if e := verifyDB(source); e != nil {
		return e
	}
	db, e := openDB(source)
	if e != nil {
		return e
	}
	defer db.Close()
	if _, e = db.Exec("VACUUM INTO ?", dest); e != nil {
		return e
	}
	if e = os.Chmod(dest, 0600); e != nil {
		return e
	}
	if e = syncFile(dest); e != nil {
		return e
	}
	return verifyDB(dest)
}
func copyFile(source, dest string) (err error) {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, out.Close()) }()
	n, err := io.Copy(out, io.LimitReader(in, maxBytes+1))
	if err != nil {
		return err
	}
	if n > maxBytes {
		return errors.New("file too large")
	}
	return out.Sync()
}
func describe(path, name string, sqlite bool) (Entry, error) {
	f, e := os.Open(path)
	if e != nil {
		return Entry{}, e
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, maxBytes+1))
	if e != nil {
		return Entry{}, e
	}
	if n > maxBytes {
		return Entry{}, errors.New("file too large")
	}
	return Entry{Path: name, Bytes: n, SHA256: hex.EncodeToString(h.Sum(nil)), SQLite: sqlite}, nil
}
func syncFile(path string) error {
	f, e := os.OpenFile(path, os.O_RDWR, 0)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

// Verify validates paths, exact inventory, hashes and database integrity before
// any destination is created. A manifest is an integrity inventory, not a signature.
func Verify(bundle string) (Manifest, error) {
	var m Manifest
	info, err := os.Lstat(bundle)
	if err != nil {
		return m, err
	}
	if !info.IsDir() {
		return m, errors.New("bundle root must be a real directory")
	}
	if _, e := os.Stat(filepath.Join(bundle, ".bundle-incomplete")); e == nil {
		return m, errors.New("incomplete bundle")
	}
	f, e := os.Open(filepath.Join(bundle, "manifest.json"))
	if e != nil {
		return m, e
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 4<<20))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&m); e != nil {
		return m, e
	}
	if m.Version != 1 || len(m.Files) == 0 || len(m.Files) > maxFiles {
		return m, errors.New("unsupported or empty manifest")
	}
	seen := map[string]bool{}
	var total int64
	for _, item := range m.Files {
		name := item.Path
		if name != "config/config.json" && name != "config/entry-budget.db" && name != "secrets.env" && !strings.HasPrefix(name, "state/") {
			return m, errors.New("invalid bundle entry")
		}
		if !fs.ValidPath(name) || strings.Contains(name, "\\") || strings.Contains(name, ":") || strings.HasSuffix(name, "/.yunqi-state.lock") || strings.HasSuffix(name, "/.bundle-incomplete") || seen[name] {
			return m, errors.New("unsafe or duplicate bundle path")
		}
		seen[name] = true
		total += item.Bytes
		if item.Bytes < 0 || total > maxBytes {
			return m, errors.New("bundle size limit exceeded")
		}
		path := filepath.Join(bundle, filepath.FromSlash(name))
		for p := path; p != filepath.Clean(bundle); p = filepath.Dir(p) {
			info, e := os.Lstat(p)
			if e != nil {
				return m, e
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return m, errors.New("symlink in bundle")
			}
		}
		info, e := os.Lstat(path)
		if e != nil {
			return m, e
		}
		if !info.Mode().IsRegular() {
			return m, errors.New("invalid bundle file")
		}
		actual, e := describe(path, name, item.SQLite)
		if e != nil {
			return m, e
		}
		if actual.Bytes != item.Bytes || actual.SHA256 != item.SHA256 {
			return m, fmt.Errorf("bundle checksum mismatch: %s", name)
		}
		sqlite, e := isSQLite(path)
		if e != nil {
			return m, e
		}
		if sqlite != item.SQLite {
			return m, errors.New("SQLite manifest mismatch")
		}
		if sqlite {
			if e = verifyDB(path); e != nil {
				return m, e
			}
		}
	}
	if !seen["config/config.json"] {
		return m, errors.New("missing config snapshot")
	}
	e = filepath.WalkDir(bundle, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(bundle, p)
		if e != nil {
			return e
		}
		name := filepath.ToSlash(rel)
		if name != "manifest.json" && !seen[name] {
			return errors.New("unlisted bundle file")
		}
		return nil
	})
	return m, e
}

// Restore only creates a new recovery root. It never overwrites a deployment,
// reloads a service, reconnects Redis or activates restored credentials.
func Restore(bundle, output string) (err error) {
	m, err := Verify(bundle)
	if err != nil {
		return err
	}
	if err = checkOutput(Options{ConfigPath: filepath.Join(bundle, "manifest.json"), StateDir: bundle}, output); err != nil {
		return err
	}
	if err = os.Mkdir(output, 0700); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(output)
		}
	}()
	configPath := filepath.Join(output, "config", "config.json")
	stateDir := filepath.Join(output, "state")
	l, err := Acquire(configPath, stateDir)
	if err != nil {
		return err
	}
	defer l.Close()
	for _, d := range []string{filepath.Dir(configPath), stateDir} {
		if err = os.WriteFile(filepath.Join(d, ".bundle-incomplete"), nil, 0600); err != nil {
			return err
		}
	}
	for _, item := range m.Files {
		dest := filepath.Join(output, filepath.FromSlash(item.Path))
		if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		if err = copyFile(filepath.Join(bundle, filepath.FromSlash(item.Path)), dest); err != nil {
			return err
		}
		actual, e := describe(dest, item.Path, item.SQLite)
		if e != nil {
			return e
		}
		if actual.SHA256 != item.SHA256 {
			return errors.New("restore checksum mismatch")
		}
	}
	for _, d := range []string{filepath.Dir(configPath), stateDir} {
		if err = os.Remove(filepath.Join(d, ".bundle-incomplete")); err != nil {
			return err
		}
	}
	return nil
}
