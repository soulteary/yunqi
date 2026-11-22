// Package statebundle implements offline, checksummed recovery bundles.
package statebundle

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
)

var ErrBusy = errors.New("state directory is in use; no files were backed up")

type Lock struct{ files []*os.File }

// Acquire excludes cooperating service instances and backup tools. Both the
// config directory and state directory participate, even when split by CLI.
// Locks are released by the OS on crash; lock files must never be deleted.
func Acquire(configPath, stateDir string) (*Lock, error) {
	dirs := []string{filepath.Dir(configPath), stateDir}
	for i, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			return nil, err
		}
		real, err := filepath.EvalSymlinks(d)
		if err != nil {
			return nil, err
		}
		a, err := filepath.Abs(real)
		if err != nil {
			return nil, err
		}
		dirs[i] = a
	}
	sort.Strings(dirs)
	l := new(Lock)
	for i, d := range dirs {
		if i > 0 && dirs[i-1] == d {
			continue
		}
		if err := os.MkdirAll(d, 0700); err != nil {
			l.Close()
			return nil, err
		}
		if _, err := os.Stat(filepath.Join(d, ".bundle-incomplete")); err == nil {
			l.Close()
			return nil, errors.New("incomplete state bundle; refuse to open")
		}
		p := filepath.Join(d, ".yunqi-state.lock")
		info, err := os.Lstat(p)
		if err == nil && !info.Mode().IsRegular() {
			l.Close()
			return nil, errors.New("invalid state lock")
		}
		if err != nil && !os.IsNotExist(err) {
			l.Close()
			return nil, err
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			l.Close()
			return nil, err
		}
		if err = lockFile(f); err != nil {
			_ = f.Close()
			l.Close()
			return nil, ErrBusy
		}
		l.files = append(l.files, f)
	}
	return l, nil
}
func (l *Lock) Close() error {
	var result error
	for _, f := range l.files {
		result = errors.Join(result, f.Close())
	}
	l.files = nil
	return result
}
