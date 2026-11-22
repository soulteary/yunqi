package entrybudget

import (
	"errors"
	"os"
)

// ValidateSharedTransition prevents silently abandoning an existing local ledger.
// An explicit acknowledgement matching the new epoch authorizes a NEW domain,
// not a migration or a reset of an existing shared domain.
func ValidateSharedTransition(localPath, epoch, newDomainAck string) error {
	info, err := os.Stat(localPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return ErrUnavailable
	}
	if !info.Mode().IsRegular() {
		return ErrUnavailable
	}
	if info.Size() > 0 && (epoch == "" || newDomainAck != epoch) {
		return errors.New("existing local budget ledger requires explicit new-domain acknowledgement or migration")
	}
	return nil
}
