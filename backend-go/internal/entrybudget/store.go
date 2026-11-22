package entrybudget

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"time"
)

const LeaseSeconds int64 = 60

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, ErrUnavailable
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	_ = file.Close()
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	for _, query := range []string{
		`CREATE TABLE IF NOT EXISTS budget_cycles(principal TEXT NOT NULL,period TEXT NOT NULL,period_start INTEGER NOT NULL,spent INTEGER NOT NULL DEFAULT 0 CHECK(spent>=0),reserved INTEGER NOT NULL DEFAULT 0 CHECK(reserved>=0),PRIMARY KEY(principal,period,period_start))`,
		`CREATE TABLE IF NOT EXISTS budget_requests(id TEXT PRIMARY KEY,principal TEXT NOT NULL,period TEXT NOT NULL,period_start INTEGER NOT NULL,period_end INTEGER NOT NULL,policy TEXT NOT NULL,created_at INTEGER NOT NULL,lease_until INTEGER NOT NULL,status TEXT NOT NULL DEFAULT 'pending',held INTEGER NOT NULL DEFAULT 0 CHECK(held>=0),charged INTEGER NOT NULL DEFAULT 0 CHECK(charged>=0),reason TEXT NOT NULL DEFAULT '',settled_at INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS budget_requests_window ON budget_requests(principal,period,status,settled_at)`,
		`CREATE INDEX IF NOT EXISTS budget_requests_owner_created ON budget_requests(principal,created_at DESC,id DESC)`,
		`CREATE INDEX IF NOT EXISTS budget_requests_expiry ON budget_requests(status,lease_until)`,
		`CREATE TABLE IF NOT EXISTS budget_attempts(id TEXT PRIMARY KEY,request_id TEXT NOT NULL REFERENCES budget_requests(id),model TEXT NOT NULL,channel_uid TEXT NOT NULL,pricing_version TEXT NOT NULL,pricing TEXT NOT NULL,quote INTEGER NOT NULL CHECK(quote>=0),held INTEGER NOT NULL CHECK(held>=0),charged INTEGER NOT NULL DEFAULT 0 CHECK(charged>=0),status TEXT NOT NULL DEFAULT 'pending',reason TEXT NOT NULL DEFAULT '',usage_source TEXT NOT NULL DEFAULT 'unknown',billing_usage TEXT NOT NULL DEFAULT '{}')`,
		`CREATE INDEX IF NOT EXISTS budget_attempts_request ON budget_attempts(request_id)`,
	} {
		if _, err := db.Exec(query); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }

type Snapshot struct {
	Policy                      Policy
	Start, End, Spent, Reserved int64
}

func (s *Store) Start(ctx context.Context, p Policy, id string, now time.Time) error {
	if err := s.RecoverExpired(ctx, now); err != nil {
		return err
	}
	window, err := p.Window(now)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(p)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO budget_cycles(principal,period,period_start) VALUES(?,?,?)`, p.PrincipalID, p.Period, window.Start); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO budget_requests(id,principal,period,period_start,period_end,policy,created_at,lease_until) VALUES(?,?,?,?,?,?,?,?)`, id, p.PrincipalID, p.Period, window.Start, window.End, string(raw), now.Unix(), now.Unix()+LeaseSeconds); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Heartbeat(ctx context.Context, id string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE budget_requests SET lease_until=? WHERE id=? AND status='pending'`, now.Unix()+LeaseSeconds, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrClosed
	}
	return nil
}

func (s *Store) Snapshot(ctx context.Context, p Policy, now time.Time) (Snapshot, error) {
	if err := s.RecoverExpired(ctx, now); err != nil {
		return Snapshot{}, err
	}
	w, err := p.Window(now)
	if err != nil {
		return Snapshot{}, err
	}
	out := Snapshot{Policy: p, Start: w.Start, End: w.End}
	err = s.db.QueryRowContext(ctx, `SELECT spent,reserved FROM budget_cycles WHERE principal=? AND period=? AND period_start=?`, p.PrincipalID, p.Period, w.Start).Scan(&out.Spent, &out.Reserved)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return out, err
	}
	if p.Period == "rolling" {
		out.Start = w.Since
		out.End = now.Unix()
		err = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(charged),0) FROM budget_requests WHERE principal=? AND period='rolling' AND status='settled' AND settled_at>=?`, p.PrincipalID, w.Since).Scan(&out.Spent)
	}
	return out, err
}

type AttemptQuote struct {
	ID, Model, ChannelUID, PricingVersion string
	Pricing                               json.RawMessage
	Amount                                int64
}

func (s *Store) Reserve(ctx context.Context, p Policy, requestID string, q AttemptQuote, now time.Time) error {
	if q.Amount < 0 || q.Amount > MaxAmount || len(q.Pricing) > 64<<10 || q.ID == "" || !json.Valid(q.Pricing) || len(q.PricingVersion) > 128 || len(q.Model) > 512 || len(q.ChannelUID) > 128 {
		return ErrPricing
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The first statement acquires SQLite's writer lock before any read, avoiding
	// deferred read->write upgrade races between independent service instances.
	result, err := tx.ExecContext(ctx, `UPDATE budget_requests SET lease_until=? WHERE id=? AND status='pending'`, now.Unix()+LeaseSeconds, requestID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrClosed
	}
	var existingRequest, version, existingPricing string
	var existingQuote int64
	err = tx.QueryRowContext(ctx, `SELECT request_id,quote,pricing_version,pricing FROM budget_attempts WHERE id=?`, q.ID).Scan(&existingRequest, &existingQuote, &version, &existingPricing)
	if err == nil {
		if existingRequest != requestID || existingQuote != q.Amount || version != q.PricingVersion || existingPricing != string(q.Pricing) {
			return ErrUnavailable
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var principal, period, storedPolicy string
	var start, held int64
	if err = tx.QueryRowContext(ctx, `SELECT principal,period,period_start,held,policy FROM budget_requests WHERE id=?`, requestID).Scan(&principal, &period, &start, &held, &storedPolicy); err != nil {
		return err
	}
	var capturedPolicy Policy
	if json.Unmarshal([]byte(storedPolicy), &capturedPolicy) != nil || capturedPolicy != p {
		return ErrClosed
	}
	if principal != p.PrincipalID || period != p.Period || held > p.RequestCapNanoUSD || q.Amount > p.RequestCapNanoUSD-held {
		return ErrQuota
	}
	var spent, reserved int64
	if err = tx.QueryRowContext(ctx, `SELECT spent,reserved FROM budget_cycles WHERE principal=? AND period=? AND period_start=?`, principal, period, start).Scan(&spent, &reserved); err != nil {
		return err
	}
	if period == "rolling" {
		since := now.Unix() - p.RollingSeconds
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(charged),0) FROM budget_requests WHERE principal=? AND period='rolling' AND status='settled' AND settled_at>=?`, principal, since).Scan(&spent); err != nil {
			return err
		}
	}
	if spent > p.LimitNanoUSD || reserved > p.LimitNanoUSD-spent || q.Amount > p.LimitNanoUSD-spent-reserved {
		return ErrQuota
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO budget_attempts(id,request_id,model,channel_uid,pricing_version,pricing,quote,held) VALUES(?,?,?,?,?,?,?,?)`, q.ID, requestID, q.Model, q.ChannelUID, q.PricingVersion, string(q.Pricing), q.Amount, q.Amount); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE budget_cycles SET reserved=reserved+? WHERE principal=? AND period=? AND period_start=?`, q.Amount, principal, period, start); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE budget_requests SET held=held+? WHERE id=?`, q.Amount, requestID); err != nil {
		return err
	}
	return tx.Commit()
}

// Complete replaces an upper-bound hold with a known charge. A nil charge is
// conservative: retain that attempt's hold, without pretending it is measured.
func (s *Store) Complete(ctx context.Context, requestID, attemptID string, charge *int64, reason string) error {
	return s.CompleteAccounting(ctx, requestID, attemptID, charge, reason, "unknown", nil)
}
func (s *Store) CompleteAccounting(ctx context.Context, requestID, attemptID string, charge *int64, reason, source string, usage json.RawMessage) error {
	if source != "provider" && source != "unknown" {
		return ErrPricing
	}
	if len(usage) == 0 {
		usage = json.RawMessage(`{}`)
	}
	if len(usage) > 128<<10 || !json.Valid(usage) {
		return ErrPricing
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE budget_requests SET held=held WHERE id=? AND status='pending'`, requestID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return nil
	}
	var principal, period, status string
	var start, requestHeld, held int64
	if err = tx.QueryRowContext(ctx, `SELECT principal,period,period_start,held FROM budget_requests WHERE id=?`, requestID).Scan(&principal, &period, &start, &requestHeld); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT held,status FROM budget_attempts WHERE id=? AND request_id=?`, attemptID, requestID).Scan(&held, &status); err != nil {
		return err
	}
	if status != "pending" {
		return nil
	}
	charged := held
	status = "conservative"
	if charge != nil {
		charged = *charge
		status = "provider"
		if charged == 0 {
			status = "released"
		}
	}
	if charged < 0 || charged > MaxAmount {
		return ErrPricing
	}
	delta := charged - held
	var cycleReserved int64
	if err = tx.QueryRowContext(ctx, `SELECT reserved FROM budget_cycles WHERE principal=? AND period=? AND period_start=?`, principal, period, start).Scan(&cycleReserved); err != nil {
		return err
	}
	if delta > 0 && (cycleReserved > math.MaxInt64-delta || requestHeld > math.MaxInt64-delta) || delta < 0 && (cycleReserved < -delta || requestHeld < -delta) {
		return ErrUnavailable
	}
	if _, err = tx.ExecContext(ctx, `UPDATE budget_cycles SET reserved=reserved+? WHERE principal=? AND period=? AND period_start=?`, delta, principal, period, start); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE budget_requests SET held=held+? WHERE id=?`, delta, requestID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE budget_attempts SET held=?,charged=?,status=?,reason=?,usage_source=?,billing_usage=? WHERE id=?`, charged, charged, status, reason, source, string(usage), attemptID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Finalize(ctx context.Context, id, reason string, now time.Time) error {
	return s.finalize(ctx, id, reason, now, false)
}
func (s *Store) finalize(ctx context.Context, id, reason string, now time.Time, expiredOnly bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := `UPDATE budget_requests SET held=held WHERE id=? AND status='pending'`
	args := []any{id}
	if expiredOnly {
		query += ` AND lease_until<=?`
		args = append(args, now.Unix())
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return nil
	}
	var principal, period string
	var start, held int64
	if err = tx.QueryRowContext(ctx, `SELECT principal,period,period_start,held FROM budget_requests WHERE id=?`, id).Scan(&principal, &period, &start, &held); err != nil {
		return err
	}
	var sum int64
	var attempts, pending, conservative int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(held),0),COUNT(*),COALESCE(SUM(status='pending'),0),COALESCE(SUM(status='conservative'),0) FROM budget_attempts WHERE request_id=?`, id).Scan(&sum, &attempts, &pending, &conservative); err != nil {
		return err
	}
	if sum != held {
		return ErrUnavailable
	}
	var spent, reserved int64
	if err = tx.QueryRowContext(ctx, `SELECT spent,reserved FROM budget_cycles WHERE principal=? AND period=? AND period_start=?`, principal, period, start).Scan(&spent, &reserved); err != nil {
		return err
	}
	if reserved < held || spent > math.MaxInt64-held {
		return ErrUnavailable
	}
	if reason == "" {
		reason = "provider_usage"
		if pending > 0 || conservative > 0 {
			reason = "conservative_usage_unavailable"
		}
		if attempts == 0 {
			reason = "no_upstream_call"
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE budget_attempts SET charged=held,status='conservative',reason='interrupted_usage_unavailable' WHERE request_id=? AND status='pending'`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE budget_cycles SET spent=spent+?,reserved=reserved-? WHERE principal=? AND period=? AND period_start=?`, held, held, principal, period, start); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE budget_requests SET charged=?,held=0,status='settled',reason=?,settled_at=? WHERE id=?`, held, reason, now.Unix(), id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE budget_attempts SET held=0 WHERE request_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) RecoverExpired(ctx context.Context, now time.Time) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM budget_requests WHERE status='pending' AND lease_until<=? LIMIT 1000`, now.Unix())
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.finalize(ctx, id, "reservation_lease_expired", now, true); err != nil {
			return err
		}
	}
	return nil
}

type RequestRecord struct {
	ID             string `json:"requestId"`
	PrincipalID    string `json:"principalId"`
	Period         string `json:"period"`
	Start          int64  `json:"periodStart"`
	End            int64  `json:"periodEnd"`
	CreatedAt      int64  `json:"createdAt"`
	SettledAt      int64  `json:"settledAt,omitempty"`
	HeldNanoUSD    int64  `json:"heldNanoUsd,string"`
	ChargedNanoUSD int64  `json:"chargedNanoUsd,string"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
}
type AttemptRecord struct {
	UsageSource    string          `json:"usageSource"`
	BillingUsage   json.RawMessage `json:"billingUsage"`
	ID             string          `json:"attemptId"`
	RequestID      string          `json:"requestId"`
	Model          string          `json:"model"`
	ChannelUID     string          `json:"channelUid"`
	PricingVersion string          `json:"pricingVersion"`
	Pricing        json.RawMessage `json:"pricingSnapshot"`
	QuotedNanoUSD  int64           `json:"quotedNanoUsd,string"`
	ChargedNanoUSD int64           `json:"chargedNanoUsd,string"`
	Status         string          `json:"status"`
	Reason         string          `json:"reason,omitempty"`
}

func (s *Store) Requests(ctx context.Context, principal string, limit int) ([]RequestRecord, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,principal,period,period_start,period_end,created_at,settled_at,held,charged,status,reason FROM budget_requests WHERE principal=? ORDER BY created_at DESC,id DESC LIMIT ?`, principal, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]RequestRecord, 0)
	for rows.Next() {
		var r RequestRecord
		if err := rows.Scan(&r.ID, &r.PrincipalID, &r.Period, &r.Start, &r.End, &r.CreatedAt, &r.SettledAt, &r.HeldNanoUSD, &r.ChargedNanoUSD, &r.Status, &r.Reason); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}
func (s *Store) Attempts(ctx context.Context, id string) ([]AttemptRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,request_id,model,channel_uid,pricing_version,pricing,quote,charged,status,reason,usage_source,billing_usage FROM budget_attempts WHERE request_id=? ORDER BY id LIMIT 1000`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AttemptRecord, 0)
	for rows.Next() {
		var r AttemptRecord
		var pricing, usage string
		if err := rows.Scan(&r.ID, &r.RequestID, &r.Model, &r.ChannelUID, &r.PricingVersion, &pricing, &r.QuotedNanoUSD, &r.ChargedNanoUSD, &r.Status, &r.Reason, &r.UsageSource, &usage); err != nil {
			return nil, err
		}
		r.BillingUsage = json.RawMessage(usage)
		r.Pricing = json.RawMessage(pricing)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RequestOwned(ctx context.Context, id, principal string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM budget_requests WHERE id=? AND principal=?`, id, principal).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
