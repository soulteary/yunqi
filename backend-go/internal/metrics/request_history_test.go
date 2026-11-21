package metrics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func historyTestStore(t testing.TB) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(&SQLiteStoreConfig{DBPath: filepath.Join(t.TempDir(), "history.db"), RetentionDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func TestRequestHistoryCursorAndFilters(t *testing.T) {
	s := historyTestStore(t)
	now := time.Now().Truncate(time.Second)
	records := []PersistentRecord{}
	for i := 0; i < 9; i++ {
		records = append(records, PersistentRecord{Timestamp: now, APIType: "messages", Model: "m", ChannelUID: "c", CorrelationID: "r", Success: true})
	}
	records = append(records, PersistentRecord{Timestamp: now, APIType: "chat", Model: "m", ChannelUID: "c", CorrelationID: "r"}, PersistentRecord{Timestamp: now, APIType: "messages", Model: "other", ChannelUID: "c", CorrelationID: "r"})
	if err := s.batchInsertRecords(records); err != nil {
		t.Fatal(err)
	}
	q := RequestHistoryQuery{APIType: "messages", Model: "m", ChannelUID: "c", CorrelationID: "r", Since: now.Add(-time.Hour), Until: now.Add(time.Hour), Limit: 2}
	seen := map[int64]bool{}
	for n := 0; ; n++ {
		p, err := s.QueryRequestHistory(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		if p.RetentionDays != 30 {
			t.Fatal("retention not reported")
		}
		for _, r := range p.Records {
			if seen[r.ID] {
				t.Fatal("duplicate", r.ID)
			}
			seen[r.ID] = true
			if r.APIType != "messages" || r.Model != "m" {
				t.Fatal("filter escaped")
			}
		}
		if n == 0 {
			if err := s.batchInsertRecords([]PersistentRecord{{Timestamp: now.Add(-time.Minute), APIType: "messages", Model: "m", ChannelUID: "c", CorrelationID: "r"}}); err != nil {
				t.Fatal(err)
			}
		}
		if p.NextCursor == "" {
			break
		}
		q.Cursor = p.NextCursor
	}
	if len(seen) != 9 {
		t.Fatalf("snapshot rows=%d", len(seen))
	}
	q.Cursor = "broken"
	if _, err := s.QueryRequestHistory(context.Background(), q); err == nil {
		t.Fatal("bad cursor accepted")
	}
	q.Cursor = ""
	p, _ := s.QueryRequestHistory(context.Background(), q)
	q.Cursor = p.NextCursor
	q.Model = "other"
	if _, err := s.QueryRequestHistory(context.Background(), q); err == nil {
		t.Fatal("cross-filter cursor accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q.Cursor = ""
	if _, err := s.QueryRequestHistory(ctx, q); err == nil {
		t.Fatal("cancel ignored")
	}
}
func TestRequestHistoryQueryPlans(t *testing.T) {
	s := historyTestStore(t)
	for _, column := range []string{"", "model", "channel_uid", "correlation_id"} {
		query := "EXPLAIN QUERY PLAN SELECT id,timestamp,api_type,model,channel_uid,correlation_id,success,input_tokens,output_tokens FROM request_records WHERE api_type='messages' AND timestamp>=0 AND timestamp<=9999999999 AND id<=99999"
		if column != "" {
			query += " AND " + column + "='x'"
		}
		query += " AND (timestamp,id)<(9999999999,99999) ORDER BY timestamp DESC,id DESC LIMIT 51"
		rows, err := s.db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		plan := ""
		for rows.Next() {
			var a, b, c int
			var d string
			if err := rows.Scan(&a, &b, &c, &d); err != nil {
				t.Fatal(err)
			}
			plan += d
		}
		rows.Close()
		t.Logf("%s: %s", column, plan)
		if !strings.Contains(plan, "INDEX idx_") || strings.Contains(plan, "TEMP B-TREE") || strings.Contains(plan, "SCAN request_records") {
			t.Fatalf("%s: %s", column, plan)
		}
	}
}
func BenchmarkRequestHistoryMillion(b *testing.B) {
	s := historyTestStore(b)
	tx, err := s.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO request_records(metrics_key,base_url,key_mask,timestamp,success,api_type,model,channel_uid,correlation_id) VALUES('k','','',?,1,'messages',?,?,?)`)
	if err != nil {
		b.Fatal(err)
	}
	now := time.Now().Unix()
	for i := 0; i < 1000000; i++ {
		if _, err := stmt.Exec(now-int64(i), fmt.Sprint(i%10), fmt.Sprint(i%100), fmt.Sprint(i%1000)); err != nil {
			b.Fatal(err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	q := RequestHistoryQuery{APIType: "messages", Since: time.Unix(now-1000001, 0), Until: time.Unix(now, 0), Limit: 50}
	// Start around row 900,000; keyset work remains proportional to page size.
	cur := historyCursor{Timestamp: now - 900000, ID: 900001, HighID: 1000000, Filter: q.fingerprint()}
	data, _ := json.Marshal(cur)
	q.Cursor = base64.RawURLEncoding.EncodeToString(data)

	b.Run("deep", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			p, err := s.QueryRequestHistory(context.Background(), q)
			if err != nil {
				b.Fatal(err)
			}
			q.Cursor = p.NextCursor
		}
	})
	b.Run("filtered", func(b *testing.B) {
		q.Model = "0"
		q.ChannelUID = "0"
		q.CorrelationID = "0"
		q.Cursor = ""
		for i := 0; i < b.N; i++ {
			p, err := s.QueryRequestHistory(context.Background(), q)
			if err != nil {
				b.Fatal(err)
			}
			q.Cursor = p.NextCursor
		}
	})
}
