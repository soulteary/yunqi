package entrybudget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Money is decoded as Go int64 and never passed through Redis Lua/JSON floating
// point numbers. WATCH/MULTI commits the entire principal's accounting mutation.
const sharedLedgerMaxBytes = 16 << 20
const sharedLedgerMaxRequests = 10000

var sharedName = regexp.MustCompile(`^[a-zA-Z0-9:_-]{1,64}$`)

type redisLedger struct {
	client        *redis.Client
	prefix, epoch string
}
type sharedCycle struct{ Spent, Reserved int64 }
type sharedRequest struct {
	Record     RequestRecord
	Policy     Policy
	LeaseUntil int64
	Attempts   map[string]*sharedAttempt
}
type sharedAttempt struct {
	Record AttemptRecord
	Held   int64
}
type sharedState struct {
	Version     int
	Principal   string
	Cycles      map[string]*sharedCycle
	Requests    map[string]*sharedRequest
	tx          *redis.Tx        `json:"-"`
	ctx         context.Context  `json:"-"`
	prefix      string           `json:"-"`
	rollingAdds map[string]int64 `json:"-"`
}

func principalDigest(principal string) string {
	sum := sha256.Sum256([]byte(principal))
	return hex.EncodeToString(sum[:])
}
func redisBudgetPrefix(namespace string) string { return namespace + ":budget:{ledger}:" }
func OpenRedis(client *redis.Client, namespace, epoch string) (*Store, error) {
	if client == nil || !sharedName.MatchString(namespace) || !sharedName.MatchString(epoch) {
		return nil, ErrUnavailable
	}
	return &Store{shared: &redisLedger{client: client, prefix: redisBudgetPrefix(namespace), epoch: epoch}}, nil
}

func (r *redisLedger) ready(ctx context.Context) error {
	pipe := r.client.Pipeline()
	epoch := pipe.Get(ctx, r.prefix+"epoch")
	registry := pipe.HGet(ctx, r.prefix+"registry", "schema")
	index := pipe.HGet(ctx, r.prefix+"requests", "schema")
	if _, err := pipe.Exec(ctx); err != nil || epoch.Val() != r.epoch || registry.Val() != "1" || index.Val() != "1" {
		return ErrUnavailable
	}
	return nil
}

// InitializeRedis is an explicit offline operation. Runtime never initializes or
// repairs a missing epoch/registry. Restoring data requires its original epoch.
func InitializeRedis(ctx context.Context, client *redis.Client, namespace, epoch string) error {
	store, err := OpenRedis(client, namespace, epoch)
	if err != nil {
		return err
	}
	r := store.shared
	return r.client.Watch(ctx, func(tx *redis.Tx) error {
		exists, err := tx.Exists(ctx, r.prefix+"epoch", r.prefix+"registry", r.prefix+"requests").Result()
		if err != nil {
			return ErrUnavailable
		}
		if exists != 0 {
			return errors.New("budget namespace already initialized; restore its original data instead")
		}
		// Do not attach a new epoch to orphaned accounting records.
		var cursor uint64
		for {
			keys, next, scanErr := tx.Scan(ctx, cursor, r.prefix+"*", 128).Result()
			if scanErr != nil || len(keys) != 0 {
				return ErrUnavailable
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
		_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
			p.Set(ctx, r.prefix+"epoch", epoch, 0)
			p.HSet(ctx, r.prefix+"registry", "schema", "1")
			p.HSet(ctx, r.prefix+"requests", "schema", "1")
			return nil
		})
		return err
	}, r.prefix+"epoch", r.prefix+"registry", r.prefix+"requests")
}
func (r *redisLedger) mutate(ctx context.Context, principal string, id string, create bool, fn func(*sharedState, time.Time) error) error {
	// Match the existing reservation/settlement RPC budget while respecting a
	// shorter parent deadline. Contenders must also validate read-only failures.
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ctx = call
	if err := ctx.Err(); err != nil {
		return ErrUnavailable
	}
	digest := principalDigest(principal)
	if principal == "" {
		var err error
		digest, err = r.client.HGet(ctx, r.prefix+"requests", id).Result()
		if err != nil {
			return ErrUnavailable
		}
	}
	key := r.prefix + "p:" + digest
	for retry := 0; retry < 32; retry++ {
		err := r.client.Watch(ctx, func(tx *redis.Tx) (result error) {
			commitAttempted := false
			defer func() {
				if result == nil || commitAttempted {
					return
				}
				if err := ctx.Err(); err != nil {
					result = err
					return
				}
				// WATCH does not make the preceding GET/HGET reads a snapshot. Even a
				// validation/business error may come from mixed generations (e.g. a
				// missing principal read before another node atomically registers it).
				// A read-only EXEC validates the watched keys before rejecting. Empty
				// pipelines skip EXEC in go-redis, so explicitly queue PING, never a write.
				_, checkErr := tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error { pipe.Ping(ctx); return nil })
				if checkErr != nil {
					result = checkErr
				}
			}()

			if err := ctx.Err(); err != nil {
				return err
			}
			epoch, err := tx.Get(ctx, r.prefix+"epoch").Result()
			if err != nil || epoch != r.epoch {
				return ErrUnavailable
			}
			schema, err := tx.HGet(ctx, r.prefix+"registry", "schema").Result()
			if err != nil || schema != "1" {
				return ErrUnavailable
			}
			raw, err := tx.Get(ctx, key).Bytes()
			state := sharedState{}
			fresh := false
			newIndex := false
			if err == redis.Nil {
				known, e := tx.HExists(ctx, r.prefix+"registry", digest).Result()
				if e != nil || known || !create || principal == "" {
					return ErrUnavailable
				}
				orphaned, orphanErr := tx.Exists(ctx, key+":history", key+":rolling", key+":rolling-index").Result()
				if orphanErr != nil || orphaned != 0 {
					return ErrUnavailable
				}
				fresh = true
				state = sharedState{Version: 1, Principal: principal, Cycles: map[string]*sharedCycle{}, Requests: map[string]*sharedRequest{}}
			} else if err != nil || len(raw) > sharedLedgerMaxBytes || json.Unmarshal(raw, &state) != nil {
				return ErrUnavailable
			}
			if state.Version != 1 || state.Principal == "" || principalDigest(state.Principal) != digest || state.Cycles == nil || state.Requests == nil {
				return ErrUnavailable
			}
			for _, cycle := range state.Cycles {
				if cycle == nil || cycle.Spent < 0 || cycle.Reserved < 0 {
					return ErrUnavailable
				}
			}
			expectedHolds := map[string]int64{}
			for requestID, request := range state.Requests {
				if request == nil || request.Attempts == nil || request.Record.ID != requestID || request.Record.PrincipalID != state.Principal || request.Record.Status != "pending" || request.Record.HeldNanoUSD < 0 || request.Record.ChargedNanoUSD < 0 {
					return ErrUnavailable
				}
				held := int64(0)
				for _, attempt := range request.Attempts {
					if attempt == nil || attempt.Held < 0 || attempt.Record.ChargedNanoUSD < 0 {
						return ErrUnavailable
					}
					held, err = addAmount(held, attempt.Held)
					if err != nil {
						return err
					}
				}
				if held != request.Record.HeldNanoUSD {
					return ErrUnavailable
				}
				cycle := cycleID(request.Record.Period, request.Record.Start)
				if state.Cycles[cycle] == nil {
					return ErrUnavailable
				}
				expectedHolds[cycle], err = addAmount(expectedHolds[cycle], held)
				if err != nil {
					return err
				}
			}
			for cycle, total := range state.Cycles {
				if total.Reserved != expectedHolds[cycle] {
					return ErrUnavailable
				}
			}

			indexSchema, indexErr := tx.HGet(ctx, r.prefix+"requests", "schema").Result()
			if indexErr != nil || indexSchema != "1" {
				return ErrUnavailable
			}
			if !fresh {
				if result := tx.ZScore(ctx, key+":history", "schema"); result.Err() != nil || result.Val() != 0 {
					return ErrUnavailable
				}
				if tx.HGet(ctx, key+":rolling", "schema").Val() != "1" || tx.ZScore(ctx, key+":rolling-index", "schema").Err() != nil {
					return ErrUnavailable
				}
			}
			state.tx = tx
			state.ctx = ctx
			state.prefix = key
			state.rollingAdds = map[string]int64{}
			if id != "" {
				owner, e := tx.HGet(ctx, r.prefix+"requests", id).Result()
				if e != nil && e != redis.Nil {
					return ErrUnavailable
				}
				newIndex = owner == ""
				if newIndex {
					archived, archiveErr := tx.Exists(ctx, r.archiveKey(id)).Result()
					if archiveErr != nil || archived != 0 {
						return ErrUnavailable
					}
					if score := tx.ZScore(ctx, key+":history", id); score.Err() != redis.Nil {
						return ErrUnavailable
					}
				}
				if owner != "" && state.Requests[id] == nil {
					raw, readErr := tx.Get(ctx, r.archiveKey(id)).Bytes()
					if readErr != nil {
						return ErrUnavailable
					}
					var archived sharedRequest
					if json.Unmarshal(raw, &archived) != nil || archived.Record.ID != id || archived.Record.PrincipalID != state.Principal || archived.Record.Status != "settled" || archived.Attempts == nil || archived.Record.HeldNanoUSD != 0 || archived.Record.ChargedNanoUSD < 0 {
						return ErrUnavailable
					}
					state.Requests[id] = &archived
				}
				if owner != "" && owner != digest {
					return ErrUnavailable
				}
			}
			now, err := tx.Time(ctx).Result()
			if err != nil {
				return ErrUnavailable
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if err = state.recover(now); err != nil {
				return err
			}
			if err = fn(&state, now); err != nil {
				return err
			}
			rollingTotals := map[string]int64{}
			for second, amount := range state.rollingAdds {
				previous, readErr := tx.HGet(ctx, key+":rolling", second).Result()
				if readErr != nil && readErr != redis.Nil {
					return ErrUnavailable
				}
				current := int64(0)
				if previous != "" {
					current, err = strconv.ParseInt(previous, 10, 64)
					if err != nil {
						return ErrUnavailable
					}
				}
				total, sumErr := addAmount(current, amount)
				if sumErr != nil {
					return sumErr
				}
				rollingTotals[second] = total
			}
			archives := map[string][]byte{}
			for requestID, req := range state.Requests {
				if req.Record.Status != "settled" {
					continue
				}
				archive, encodeErr := json.Marshal(req)
				if encodeErr != nil {
					return ErrUnavailable
				}
				archiveKey := r.archiveKey(requestID)
				if watchErr := tx.Watch(ctx, archiveKey).Err(); watchErr != nil {
					return ErrUnavailable
				}
				old, readErr := tx.Get(ctx, archiveKey).Bytes()
				if readErr != nil && readErr != redis.Nil {
					return ErrUnavailable
				}
				if readErr == nil && string(old) != string(archive) {
					return ErrUnavailable
				}
				if readErr == redis.Nil {
					archives[archiveKey] = archive
				}
				delete(state.Requests, requestID)
			}
			oldBuckets, bucketErr := tx.ZRangeByScore(ctx, key+":rolling-index", &redis.ZRangeBy{Min: "1", Max: strconv.FormatInt(now.Unix()-30*86400-1, 10)}).Result()
			if bucketErr != nil {
				return ErrUnavailable
			}
			encoded, err := json.Marshal(state)
			if err != nil || len(encoded) > sharedLedgerMaxBytes {
				return ErrUnavailable
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			commitAttempted = true
			_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				p.Set(ctx, key, encoded, 0)
				for archiveKey, archive := range archives {
					p.Set(ctx, archiveKey, archive, 0)
				}
				if fresh {
					p.ZAdd(ctx, key+":history", redis.Z{Score: 0, Member: "schema"})
					p.HSet(ctx, key+":rolling", "schema", "1")
					p.ZAdd(ctx, key+":rolling-index", redis.Z{Score: 0, Member: "schema"})
				}
				if newIndex && id != "" {
					p.ZAdd(ctx, key+":history", redis.Z{Score: float64(now.Unix()), Member: id})
				}
				for second, amount := range rollingTotals {
					p.HSet(ctx, key+":rolling", second, strconv.FormatInt(amount, 10))
					stamp, _ := strconv.ParseInt(second, 10, 64)
					p.ZAdd(ctx, key+":rolling-index", redis.Z{Score: float64(stamp), Member: second})
				}
				if len(oldBuckets) > 0 {
					p.HDel(ctx, key+":rolling", oldBuckets...)
					p.ZRemRangeByScore(ctx, key+":rolling-index", "1", strconv.FormatInt(now.Unix()-30*86400-1, 10))
				}
				if fresh {
					p.HSet(ctx, r.prefix+"registry", digest, "1")
				}
				if newIndex {
					p.HSet(ctx, r.prefix+"requests", id, digest)
				}
				return ctx.Err()
			})
			return err
		}, key, key+":history", key+":rolling", key+":rolling-index", r.prefix+"epoch", r.prefix+"registry", r.prefix+"requests")
		if err == nil || errors.Is(err, ErrQuota) || errors.Is(err, ErrPricing) || errors.Is(err, ErrClosed) {
			return err
		}
		if err != redis.TxFailedErr {
			return ErrUnavailable
		}
		select {
		case <-ctx.Done():
			return ErrUnavailable
		case <-time.After(time.Duration(1+rand.IntN(retry+1)) * time.Millisecond):
		}
	}
	return ErrUnavailable
}
func (r *redisLedger) archiveKey(id string) string {
	return r.prefix + "archive:" + principalDigest(id)
}
func cycleID(period string, start int64) string {
	b, _ := json.Marshal([]any{period, start})
	return string(b)
}
func (s *sharedState) cycle(req *sharedRequest) (*sharedCycle, error) {
	c := s.Cycles[cycleID(req.Record.Period, req.Record.Start)]
	if c == nil {
		return nil, ErrUnavailable
	}
	return c, nil
}
func addAmount(a, b int64) (int64, error) {
	if a < 0 || b > 0 && a > math.MaxInt64-b || b < 0 && (b == math.MinInt64 || a < -b) {
		return 0, ErrUnavailable
	}
	return a + b, nil
}
func (s *sharedState) settle(req *sharedRequest, reason string, now time.Time) error {
	if req.Record.Status != "pending" {
		return nil
	}
	c, err := s.cycle(req)
	if err != nil {
		return err
	}
	var held int64
	pending := false
	for _, a := range req.Attempts {
		held, err = addAmount(held, a.Held)
		if err != nil {
			return err
		}
		if a.Record.Status == "pending" || a.Record.Status == "conservative" {
			pending = true
		}
	}
	if held != req.Record.HeldNanoUSD || c.Reserved < held {
		return ErrUnavailable
	}
	spent, err := addAmount(c.Spent, held)
	if err != nil {
		return err
	}
	if reason == "" {
		reason = "provider_usage"
		if pending {
			reason = "conservative_usage_unavailable"
		}
		if len(req.Attempts) == 0 {
			reason = "no_upstream_call"
		}
	}
	for _, a := range req.Attempts {
		if a.Record.Status == "pending" {
			a.Record.Status = "conservative"
			a.Record.Reason = "interrupted_usage_unavailable"
			a.Record.ChargedNanoUSD = a.Held
		}
		a.Held = 0
	}
	if req.Record.Period == "rolling" {
		second := strconv.FormatInt(now.Unix(), 10)
		total, e := addAmount(s.rollingAdds[second], held)
		if e != nil {
			return e
		}
		s.rollingAdds[second] = total
	}
	c.Spent = spent
	c.Reserved -= held
	req.Record.ChargedNanoUSD = held
	req.Record.HeldNanoUSD = 0
	req.Record.Status = "settled"
	req.Record.Reason = reason
	req.Record.SettledAt = now.Unix()
	return nil
}
func (s *sharedState) recover(now time.Time) error {
	for _, req := range s.Requests {
		if req.Record.Status == "pending" && req.LeaseUntil <= now.Unix() {
			if err := s.settle(req, "reservation_lease_expired", now); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *sharedState) rollingSpent(since int64) (int64, error) {
	var spent int64
	seconds, err := s.tx.ZRangeByScore(s.ctx, s.prefix+":rolling-index", &redis.ZRangeBy{Min: strconv.FormatInt(since, 10), Max: "+inf"}).Result()
	if err != nil {
		return 0, ErrUnavailable
	}
	if len(seconds) > 0 {
		values, err := s.tx.HMGet(s.ctx, s.prefix+":rolling", seconds...).Result()
		if err != nil {
			return 0, ErrUnavailable
		}
		for _, raw := range values {
			text, ok := raw.(string)
			if !ok {
				return 0, ErrUnavailable
			}
			amount, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				return 0, ErrUnavailable
			}
			spent, err = addAmount(spent, amount)
			if err != nil {
				return 0, err
			}
		}
	}
	for second, amount := range s.rollingAdds {
		stamp, _ := strconv.ParseInt(second, 10, 64)
		if stamp >= since {
			var err error
			spent, err = addAmount(spent, amount)
			if err != nil {
				return 0, err
			}
		}
	}
	return spent, nil
}

func (r *redisLedger) Start(ctx context.Context, p Policy, id string, _ time.Time) error {
	if id == "" || id == "schema" || len(id) > 256 {
		return ErrUnavailable
	}
	return r.mutate(ctx, p.PrincipalID, id, true, func(s *sharedState, now time.Time) error {
		if _, ok := s.Requests[id]; ok {
			return ErrUnavailable
		}
		pendingCount := 0
		for _, request := range s.Requests {
			if request.Record.Status == "pending" {
				pendingCount++
			}
		}
		if pendingCount >= sharedLedgerMaxRequests {
			return ErrUnavailable
		}
		w, err := p.Window(now)
		if err != nil {
			return err
		}
		key := cycleID(p.Period, w.Start)
		if s.Cycles[key] == nil {
			s.Cycles[key] = &sharedCycle{}
		}
		s.Requests[id] = &sharedRequest{Record: RequestRecord{ID: id, PrincipalID: p.PrincipalID, Period: p.Period, Start: w.Start, End: w.End, CreatedAt: now.Unix(), Status: "pending"}, Policy: p, LeaseUntil: now.Unix() + LeaseSeconds, Attempts: map[string]*sharedAttempt{}}
		return nil
	})
}
func (r *redisLedger) Heartbeat(ctx context.Context, id string, _ time.Time) error {
	return r.mutate(ctx, "", id, false, func(s *sharedState, now time.Time) error {
		req := s.Requests[id]
		if req == nil || req.Record.Status != "pending" {
			return ErrClosed
		}
		req.LeaseUntil = now.Unix() + LeaseSeconds
		return nil
	})
}
func (r *redisLedger) Snapshot(ctx context.Context, p Policy, _ time.Time) (Snapshot, error) {
	out := Snapshot{Policy: p}
	err := r.mutate(ctx, p.PrincipalID, "", true, func(s *sharedState, now time.Time) error {
		w, err := p.Window(now)
		if err != nil {
			return err
		}
		out = Snapshot{Policy: p, Start: w.Start, End: w.End}
		if c := s.Cycles[cycleID(p.Period, w.Start)]; c != nil {
			out.Spent = c.Spent
			out.Reserved = c.Reserved
		}
		if p.Period == "rolling" {
			out.Start = w.Since
			out.End = now.Unix()
			out.Spent, err = s.rollingSpent(w.Since)
		}
		return err
	})
	return out, err
}
func (r *redisLedger) Reserve(ctx context.Context, p Policy, id string, q AttemptQuote, _ time.Time) error {
	if q.Amount < 0 || q.Amount > MaxAmount || len(q.Pricing) > 64<<10 || q.ID == "" || !json.Valid(q.Pricing) || len(q.PricingVersion) > 128 || len(q.Model) > 512 || len(q.ChannelUID) > 128 {
		return ErrPricing
	}
	return r.mutate(ctx, "", id, false, func(s *sharedState, now time.Time) error {
		req := s.Requests[id]
		if req == nil || req.Record.Status != "pending" || req.Policy != p {
			return ErrClosed
		}
		if a := req.Attempts[q.ID]; a != nil {
			if a.Record.QuotedNanoUSD != q.Amount || a.Record.PricingVersion != q.PricingVersion || string(a.Record.Pricing) != string(q.Pricing) {
				return ErrUnavailable
			}
			return nil
		}
		if len(req.Attempts) >= 1000 {
			return ErrUnavailable
		}
		c, err := s.cycle(req)
		if err != nil {
			return err
		}
		spent := c.Spent
		if p.Period == "rolling" {
			spent, err = s.rollingSpent(now.Unix() - p.RollingSeconds)
			if err != nil {
				return err
			}
		}
		if req.Record.HeldNanoUSD > p.RequestCapNanoUSD || q.Amount > p.RequestCapNanoUSD-req.Record.HeldNanoUSD || spent > p.LimitNanoUSD || c.Reserved > p.LimitNanoUSD-spent || q.Amount > p.LimitNanoUSD-spent-c.Reserved {
			return ErrQuota
		}
		c.Reserved += q.Amount
		req.Record.HeldNanoUSD += q.Amount
		req.LeaseUntil = now.Unix() + LeaseSeconds
		req.Attempts[q.ID] = &sharedAttempt{Record: AttemptRecord{ID: q.ID, RequestID: id, Model: q.Model, ChannelUID: q.ChannelUID, PricingVersion: q.PricingVersion, Pricing: append(json.RawMessage(nil), q.Pricing...), QuotedNanoUSD: q.Amount, Status: "pending", UsageSource: "unknown", BillingUsage: json.RawMessage(`{}`)}, Held: q.Amount}
		return nil
	})
}
func (r *redisLedger) CompleteAccounting(ctx context.Context, id, attempt string, charge *int64, reason, source string, usage json.RawMessage) error {
	if source != "provider" && source != "unknown" {
		return ErrPricing
	}
	if len(usage) == 0 {
		usage = json.RawMessage(`{}`)
	}
	if len(usage) > 128<<10 || !json.Valid(usage) {
		return ErrPricing
	}
	if charge != nil && (*charge < 0 || *charge > MaxAmount) {
		return ErrPricing
	}
	return r.mutate(ctx, "", id, false, func(s *sharedState, _ time.Time) error {
		req := s.Requests[id]
		if req == nil {
			return ErrUnavailable
		}
		if req.Record.Status != "pending" {
			return nil
		}
		a := req.Attempts[attempt]
		if a == nil {
			return ErrUnavailable
		}
		if a.Record.Status != "pending" {
			return nil
		}
		c, err := s.cycle(req)
		if err != nil {
			return err
		}
		charged := a.Held
		status := "conservative"
		if charge != nil {
			charged = *charge
			status = "provider"
			if charged == 0 {
				status = "released"
			}
		}
		delta := charged - a.Held
		reserved, err := addAmount(c.Reserved, delta)
		if err != nil {
			return err
		}
		held, err := addAmount(req.Record.HeldNanoUSD, delta)
		if err != nil {
			return err
		}
		c.Reserved = reserved
		req.Record.HeldNanoUSD = held
		a.Held = charged
		a.Record.ChargedNanoUSD = charged
		a.Record.Status = status
		a.Record.Reason = reason
		a.Record.UsageSource = source
		a.Record.BillingUsage = append(json.RawMessage(nil), usage...)
		return nil
	})
}
func (r *redisLedger) Finalize(ctx context.Context, id, reason string, _ time.Time) error {
	return r.mutate(ctx, "", id, false, func(s *sharedState, now time.Time) error {
		req := s.Requests[id]
		if req == nil {
			return ErrUnavailable
		}
		return s.settle(req, reason, now)
	})
}
func (r *redisLedger) RecoverExpired(ctx context.Context, _ time.Time) error {
	// All normal operations recover their principal atomically. Explicit recovery
	// visits only registry entries; it never guesses missing data from key expiry.
	members, err := r.client.HKeys(ctx, r.prefix+"registry").Result()
	if err != nil {
		return ErrUnavailable
	}
	for _, digest := range members {
		if digest == "schema" {
			continue
		}
		raw, err := r.client.Get(ctx, r.prefix+"p:"+digest).Bytes()
		if err != nil {
			return ErrUnavailable
		}
		var s sharedState
		if json.Unmarshal(raw, &s) != nil {
			return ErrUnavailable
		}
		if err = r.mutate(ctx, s.Principal, "", false, func(*sharedState, time.Time) error { return nil }); err != nil {
			return err
		}
	}
	return nil
}
func (r *redisLedger) Requests(ctx context.Context, principal string, limit int) ([]RequestRecord, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	out := []RequestRecord{}
	err := r.mutate(ctx, principal, "", true, func(s *sharedState, _ time.Time) error {
		out = []RequestRecord{}
		ids, err := s.tx.ZRevRangeByScore(s.ctx, s.prefix+":history", &redis.ZRangeBy{Min: "1", Max: "+inf", Count: int64(limit)}).Result()
		if err != nil {
			return ErrUnavailable
		}
		for _, id := range ids {
			if req := s.Requests[id]; req != nil {
				out = append(out, req.Record)
				continue
			}
			raw, err := s.tx.Get(s.ctx, r.archiveKey(id)).Bytes()
			if err != nil {
				return ErrUnavailable
			}
			var archived struct{ Record RequestRecord }
			if json.Unmarshal(raw, &archived) != nil || archived.Record.PrincipalID != principal || archived.Record.ID != id {
				return ErrUnavailable
			}
			out = append(out, archived.Record)
		}

		return nil
	})
	return out, err
}
func (r *redisLedger) Attempts(ctx context.Context, id string) ([]AttemptRecord, error) {
	out := []AttemptRecord{}
	err := r.mutate(ctx, "", id, false, func(s *sharedState, _ time.Time) error {
		req := s.Requests[id]
		if req == nil {
			return ErrUnavailable
		}
		out = []AttemptRecord{}
		for _, a := range req.Attempts {
			out = append(out, a.Record)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return nil
	})
	return out, err
}
func (r *redisLedger) RequestOwned(ctx context.Context, id, principal string) (bool, error) {
	owner, err := r.client.HGet(ctx, r.prefix+"requests", id).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, ErrUnavailable
	}
	if owner != principalDigest(principal) {
		return false, nil
	}
	found := false
	err = r.mutate(ctx, principal, id, false, func(s *sharedState, _ time.Time) error { _, found = s.Requests[id]; return nil })
	return found, err
}
