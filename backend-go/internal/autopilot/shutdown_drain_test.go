package autopilot

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestAsyncWriterShutdownPersistsPartialBatchExactlyOnce(t *testing.T) {
	db := &mockTraceDB{}
	w := newAsyncWriter(db)
	for i := 0; i < 11; i++ {
		w.enqueue(fmt.Sprint(i), []byte(`{}`), priorityTerminal)
	}
	w.close()
	w.enqueue("after-close", []byte(`{}`), priorityTerminal)
	w.close()
	db.mu.Lock()
	defer db.mu.Unlock()
	if len(db.execCalls) != 11 {
		t.Fatalf("writes=%d, expected 11", len(db.execCalls))
	}
	ids := map[string]bool{}
	for _, call := range db.execCalls {
		id := call.args[0].(string)
		if id == "" || ids[id] {
			t.Fatalf("phantom or repeated write: %q", id)
		}
		ids[id] = true
	}
}
func TestAsyncWriterConcurrentShutdown(t *testing.T) {
	db := &mockTraceDB{}
	w := newAsyncWriter(db)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				w.enqueue("x", []byte(`{}`), priorityNormal)
			}
		}()
	}
	w.close()
	wg.Wait()
}

func TestManagerCloseDrainsBeforeSharedDatabaseClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	store, err := NewProfileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := NewTraceStoreWithDB(store.DB())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 11; i++ {
		trace.writer.enqueue(fmt.Sprint(i), []byte(`{}`), priorityTerminal)
	}
	manager := &Manager{store: store, traceStore: trace}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewProfileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var count int
	if err := reopened.DB().QueryRow("SELECT COUNT(*) FROM autopilot_routing_traces").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 11 {
		t.Fatalf("persisted traces=%d want11", count)
	}
}
