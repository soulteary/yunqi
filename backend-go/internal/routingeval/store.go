package routingeval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const RetentionLimit = 100

// Store retains fixed datasets and their derived reports in the existing
// autopilot database. Evaluation writes never affect routing or shadow sampling.
type Store struct{ db *sql.DB }
type Saved struct {
	Report    Report `json:"report"`
	CreatedAt int64  `json:"createdAt"`
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("evaluation persistence unavailable")
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS routing_evaluations(id TEXT PRIMARY KEY, dataset TEXT NOT NULL, report TEXT NOT NULL, created_at INTEGER NOT NULL)`)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}
func (s *Store) Save(ctx context.Context, d Dataset) (Saved, error) {
	r, err := Evaluate(d)
	if err != nil {
		return Saved{}, err
	}
	input, err := json.Marshal(d)
	if err != nil {
		return Saved{}, err
	}
	if len(input) > 4<<20 {
		return Saved{}, errors.New("evaluation too large")
	}
	output, err := json.Marshal(r)
	if err != nil {
		return Saved{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Saved{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Unix()
	if _, err = tx.ExecContext(ctx, `INSERT INTO routing_evaluations(id,dataset,report,created_at) VALUES(?,?,?,?) ON CONFLICT(id) DO NOTHING`, r.ID, string(input), string(output), now); err != nil {
		return Saved{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM routing_evaluations WHERE id NOT IN (SELECT id FROM routing_evaluations ORDER BY rowid DESC LIMIT ?)`, RetentionLimit); err != nil {
		return Saved{}, err
	}
	if err = tx.Commit(); err != nil {
		return Saved{}, err
	}
	return s.Get(ctx, r.ID)
}
func (s *Store) Get(ctx context.Context, id string) (Saved, error) {
	if len(id) != 64 {
		return Saved{}, sql.ErrNoRows
	}
	var raw string
	var saved Saved
	if err := s.db.QueryRowContext(ctx, `SELECT report,created_at FROM routing_evaluations WHERE id=?`, id).Scan(&raw, &saved.CreatedAt); err != nil {
		return Saved{}, err
	}
	if err := json.Unmarshal([]byte(raw), &saved.Report); err != nil {
		return Saved{}, err
	}
	return saved, nil
}
func (s *Store) Dataset(ctx context.Context, id string) (Dataset, error) {
	var d Dataset
	var raw string
	if len(id) != 64 {
		return d, sql.ErrNoRows
	}
	if err := s.db.QueryRowContext(ctx, `SELECT dataset FROM routing_evaluations WHERE id=?`, id).Scan(&raw); err != nil {
		return d, err
	}
	err := json.Unmarshal([]byte(raw), &d)
	return d, err
}
func (s *Store) List(ctx context.Context) ([]Saved, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT report,created_at FROM routing_evaluations ORDER BY rowid DESC LIMIT ?`, RetentionLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Saved{}
	for rows.Next() {
		var raw string
		var item Saved
		if err = rows.Scan(&raw, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &item.Report); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
