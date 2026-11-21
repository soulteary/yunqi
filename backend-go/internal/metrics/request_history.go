package metrics

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrHistoryCursor = errors.New("invalid history cursor")

// RequestHistoryQuery selects persisted metadata only. Cursor is bound to filters
// and a high-water ID, so inserts during paging cannot shift or duplicate pages.
type RequestHistoryQuery struct {
	APIType       string
	Model         string
	ChannelUID    string
	CorrelationID string
	Since         time.Time
	Until         time.Time
	Limit         int
	Cursor        string
}
type RequestHistoryEntry struct {
	ID            int64  `json:"id"`
	Timestamp     int64  `json:"timestamp"`
	APIType       string `json:"type"`
	Model         string `json:"model"`
	ChannelUID    string `json:"channelUid"`
	CorrelationID string `json:"correlationId"`
	Success       bool   `json:"success"`
	InputTokens   int64  `json:"inputTokens"`
	OutputTokens  int64  `json:"outputTokens"`
}
type RequestHistoryPage struct {
	Records       []RequestHistoryEntry `json:"records"`
	NextCursor    string                `json:"nextCursor,omitempty"`
	RetentionDays int                   `json:"retentionDays"`
}
type historyCursor struct {
	Timestamp int64  `json:"t"`
	ID        int64  `json:"i"`
	HighID    int64  `json:"h"`
	Filter    string `json:"f"`
}

func (q RequestHistoryQuery) fingerprint() string {
	b, _ := json.Marshal([]any{q.APIType, q.Model, q.ChannelUID, q.CorrelationID, q.Since.Unix(), q.Until.Unix()})
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func (s *SQLiteStore) QueryRequestHistory(ctx context.Context, q RequestHistoryQuery) (RequestHistoryPage, error) {
	page := RequestHistoryPage{Records: []RequestHistoryEntry{}, RetentionDays: s.retentionDays}
	switch q.APIType {
	case "messages", "responses", "chat", "gemini", "images", "vectors":
	default:
		return page, fmt.Errorf("invalid type")
	}
	if q.Limit < 1 || q.Limit > 200 || q.Since.IsZero() || q.Until.Before(q.Since) {
		return page, fmt.Errorf("invalid history range or limit")
	}
	cur := historyCursor{Filter: q.fingerprint()}
	if q.Cursor != "" {
		if len(q.Cursor) > 1024 {
			return page, ErrHistoryCursor
		}
		b, e := base64.RawURLEncoding.DecodeString(q.Cursor)
		if e != nil || json.Unmarshal(b, &cur) != nil || cur.Filter != q.fingerprint() || cur.ID <= 0 || cur.HighID < cur.ID {
			return page, ErrHistoryCursor
		}
	} else if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM request_records").Scan(&cur.HighID); err != nil {
		return page, err
	}
	where := []string{"api_type = ?", "timestamp >= ?", "timestamp <= ?", "id <= ?"}
	until := q.Until.Unix()
	if q.Cursor != "" && cur.Timestamp < until {
		until = cur.Timestamp
	}
	args := []any{q.APIType, q.Since.Unix(), until, cur.HighID}
	for _, f := range []struct{ column, value string }{{"model", q.Model}, {"channel_uid", q.ChannelUID}, {"correlation_id", q.CorrelationID}} {
		if f.value != "" {
			where = append(where, f.column+" = ?")
			args = append(args, f.value)
		}
	}
	if q.Cursor != "" {
		// SQLite may not infer a scalar seek bound from a tuple comparison
		// alongside another timestamp range. Make it explicit for deep pages.
		where = append(where, "(timestamp,id) < (?,?)")
		args = append(args, cur.Timestamp, cur.ID)
	}
	args = append(args, q.Limit+1)
	rows, err := s.db.QueryContext(ctx, `SELECT id,timestamp,api_type,model,channel_uid,correlation_id,success,input_tokens,output_tokens FROM `+historyTable(q)+` WHERE `+strings.Join(where, " AND ")+` ORDER BY timestamp DESC,id DESC LIMIT ?`, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var r RequestHistoryEntry
		if err = rows.Scan(&r.ID, &r.Timestamp, &r.APIType, &r.Model, &r.ChannelUID, &r.CorrelationID, &r.Success, &r.InputTokens, &r.OutputTokens); err != nil {
			return page, err
		}
		page.Records = append(page.Records, r)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Records) > q.Limit {
		page.Records = page.Records[:q.Limit]
		last := page.Records[q.Limit-1]
		cur.ID = last.ID
		cur.Timestamp = last.Timestamp
		b, _ := json.Marshal(cur)
		page.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	return page, nil
}

// With no ANALYZE statistics SQLite can prefer the broad time index even for an
// exact correlation/model filter. Use only fixed, known indexes for each query.
func historyTable(q RequestHistoryQuery) string {
	switch {
	case q.CorrelationID != "":
		return "request_records INDEXED BY idx_history_correlation_id"
	case q.ChannelUID != "":
		return "request_records INDEXED BY idx_history_channel_uid"
	case q.Model != "":
		return "request_records INDEXED BY idx_history_model"
	default:
		return "request_records"
	}
}
