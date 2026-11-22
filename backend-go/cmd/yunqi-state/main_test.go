package main

import (
	"io"
	"testing"
)

func TestStateCLIRequiresExplicitColdBackupAndNewDestination(t *testing.T) {
	for _, args := range [][]string{nil, {"backup"}, {"backup", "--output", "unused"}, {"restore"}, {"verify"}, {"unexpected"}, {"verify", "extra"}} {
		if e := run(args, io.Discard); e == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
