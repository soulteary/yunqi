package metrics

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSQLiteShutdownPersistsAcknowledgedRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.db")
	s, err := NewSQLiteStore(&SQLiteStoreConfig{DBPath: path})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 127; j++ {
				s.AddRecord(PersistentRecord{MetricsKey: fmt.Sprint(i), Timestamp: time.Now(), APIType: "messages", CorrelationID: fmt.Sprintf("%d:%d", i, j)})
			}
		}(i)
	}
	wg.Wait()
	var closes sync.WaitGroup
	for i := 0; i < 4; i++ {
		closes.Add(1)
		go func() {
			defer closes.Done()
			if err := s.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	closes.Wait()
	r, err := NewSQLiteStore(&SQLiteStoreConfig{DBPath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var count, distinct int
	if err := r.db.QueryRow("SELECT COUNT(*),COUNT(DISTINCT correlation_id) FROM request_records").Scan(&count, &distinct); err != nil {
		t.Fatal(err)
	}
	if count != 8*127 || distinct != count {
		t.Fatalf("count=%d distinct=%d", count, distinct)
	}
}
func TestSQLiteAddDuringCloseDoesNotRace(t *testing.T) {
	s, err := NewSQLiteStore(&SQLiteStoreConfig{DBPath: filepath.Join(t.TempDir(), "m.db")})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 150; j++ {
				s.AddRecord(PersistentRecord{Timestamp: time.Now(), APIType: "messages"})
			}
		}()
	}
	s.Close()
	wg.Wait()
}
