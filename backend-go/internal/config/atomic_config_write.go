package config

import (
	"os"
	"path/filepath"
)

// writeConfigAtomically leaves the previous complete configuration intact on
// write/flush/close failure. The watcher observes the final rename in its directory.
func writeConfigAtomically(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".config-save-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
