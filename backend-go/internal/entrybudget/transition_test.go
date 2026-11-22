package entrybudget

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSharedTransitionCannotSilentlyDiscardLocalLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entry-budget.db")
	if err := ValidateSharedTransition(path, "new-epoch", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("existing-accounting-data"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, ack := range []string{"", "true", "different-epoch"} {
		if err := ValidateSharedTransition(path, "new-epoch", ack); err == nil {
			t.Fatalf("local ledger discarded with ack=%q", ack)
		}
	}
	if err := ValidateSharedTransition(path, "new-epoch", "new-epoch"); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	if string(original) != "existing-accounting-data" {
		t.Fatal("transition modified old ledger")
	}
}
