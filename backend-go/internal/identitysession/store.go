// Package identitysession provides shared, monotonic session revocations.
package identitysession

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/redisconn"
)

var ErrUnavailable = errors.New("shared identity revocation unavailable")
var namespacePattern = regexp.MustCompile(`^[a-zA-Z0-9:_-]{1,64}$`)

const maxDateUnix int64 = 253402300799
const prepareLua = `
local epoch=redis.call('GET',KEYS[1])
if not epoch then
 if ARGV[1]~='' or ARGV[2]~='1' then return redis.error_reply('identity revocation epoch unavailable') end
 redis.call('SET',KEYS[1],ARGV[3],'NX')
 epoch=redis.call('GET',KEYS[1])
end
if ARGV[1]~='' and epoch~=ARGV[1] then return redis.error_reply('identity revocation epoch mismatch') end
return epoch
`
const revokedLua = `
if redis.call('GET',KEYS[1])~=ARGV[1] then return redis.error_reply('identity revocation epoch unavailable') end
return redis.call('EXISTS',KEYS[2])
`
const revokeLua = `
if redis.call('GET',KEYS[2])~=ARGV[2] then return redis.error_reply('identity revocation epoch unavailable') end

local expiry=tonumber(ARGV[1])
if expiry==0 then redis.call('SET',KEYS[1],'1');return 1 end
local clock=redis.call('TIME')
local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
-- Keep a five-minute retention margin; authentication expiry remains strict.
local remaining=math.max(300000,expiry*1000+300000-now)
local current=redis.call('PTTL',KEYS[1])
if current==-1 then return 1 end
if current<remaining then redis.call('SET',KEYS[1],'1','PX',string.format('%.0f',remaining)) end
return 1
`

var revokeScript = redis.NewScript(revokeLua)
var prepareScript = redis.NewScript(prepareLua)
var revokedScript = redis.NewScript(revokedLua)

type Store struct {
	client    *redis.Client
	namespace string
	syncGate  chan struct{}
	synced    string
	epoch     atomic.Value
}

func New(rawURL, namespace string) (*Store, error) {
	if namespace == "" {
		namespace = "yunqi"
	}
	if !namespacePattern.MatchString(namespace) {
		return nil, ErrUnavailable
	}
	client, err := redisconn.New(rawURL)
	if err != nil {
		return nil, ErrUnavailable
	}
	store := &Store{client: client, namespace: namespace, syncGate: make(chan struct{}, 1)}
	store.epoch.Store("")
	return store, nil
}
func canonical(hash string, expires int64) (string, int64, error) {
	hash = strings.ToLower(hash)
	value, err := hex.DecodeString(hash)
	if err != nil || len(value) != 32 || expires < 0 {
		return "", 0, ErrUnavailable
	}
	// Existing local policies may use arbitrarily distant finite expiries. Treat
	// those as permanent rather than losing precision in Redis's Lua numbers.
	if expires > maxDateUnix {
		expires = 0
	}
	return hash, expires, nil
}
func (s *Store) key(hash string) string { return s.namespace + ":identity:revoked:" + hash }
func (s *Store) epochKey() string       { return s.key("epoch") }
func (s *Store) pinnedEpoch() string    { return s.epoch.Load().(string) }

// Only an explicit first local->shared administrator activation may create a
// realm. Startup/authentication and already bound configurations never recreate.
func (s *Store) Prepare(ctx context.Context, expected string, initialize bool) (string, error) {
	expected = strings.ToLower(expected)
	pinned := s.pinnedEpoch()
	if expected != "" {
		if _, _, err := canonical(expected, 0); err != nil {
			return "", ErrUnavailable
		}
	}
	if pinned != "" {
		if expected != "" && pinned != expected {
			return "", ErrUnavailable
		}
		expected = pinned
	}
	if expected == "" && !initialize {
		return "", ErrUnavailable
	}
	candidate := ""
	allow := "0"
	if initialize && expected == "" {
		var nonce [32]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return "", ErrUnavailable
		}
		candidate = hex.EncodeToString(nonce[:])
		allow = "1"
	}
	epoch, err := prepareScript.Run(ctx, s.client, []string{s.epochKey()}, expected, allow, candidate).Text()
	if err != nil {
		return "", ErrUnavailable
	}
	if _, _, err := canonical(epoch, 0); err != nil {
		return "", ErrUnavailable
	}
	if !s.epoch.CompareAndSwap(pinned, epoch) && s.pinnedEpoch() != epoch {
		return "", ErrUnavailable
	}
	return epoch, nil
}
func (s *Store) IsRevoked(ctx context.Context, hash string) (bool, error) {
	hash, _, err := canonical(hash, 0)
	if err != nil {
		return false, err
	}
	epoch := s.pinnedEpoch()
	if epoch == "" {
		return false, ErrUnavailable
	}
	count, err := revokedScript.Run(ctx, s.client, []string{s.epochKey(), s.key(hash)}, epoch).Int64()
	if err != nil {
		return false, ErrUnavailable
	}
	return count > 0, nil
}
func (s *Store) Revoke(ctx context.Context, hash string, expires int64) error {
	hash, expires, err := canonical(hash, expires)
	if err != nil {
		return err
	}
	epoch := s.pinnedEpoch()
	if epoch == "" {
		return ErrUnavailable
	}
	if _, err := revokeScript.Run(ctx, s.client, []string{s.key(hash), s.epochKey()}, expires, epoch).Result(); err != nil {
		return ErrUnavailable
	}
	return nil
}

// Sync only adds/extends records; old clients or snapshots cannot remove a
// revocation written by another node. A failed import never authorizes a user.
func (s *Store) Sync(ctx context.Context, records []config.IdentitySessionRevocation) error {
	if s.pinnedEpoch() == "" {
		return ErrUnavailable
	}
	if len(records) > 4096 {
		return ErrUnavailable
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		return ErrUnavailable
	}
	sum := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(sum[:])
	select {
	case s.syncGate <- struct{}{}:
	case <-ctx.Done():
		return ErrUnavailable
	}
	defer func() { <-s.syncGate }()
	if s.synced == fingerprint {
		return nil
	}
	_, err = s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		now := time.Now().Unix()
		for _, record := range records {
			if record.ExpiresAt > 0 && record.ExpiresAt <= now {
				continue
			}
			hash, expires, err := canonical(record.Hash, record.ExpiresAt)
			if err != nil {
				return err
			}
			pipe.Eval(ctx, revokeLua, []string{s.key(hash), s.epochKey()}, expires, s.pinnedEpoch())
		}
		return nil
	})
	if err != nil {
		return ErrUnavailable
	}
	s.synced = fingerprint
	return nil
}
func (s *Store) Close() error { return s.client.Close() }
