package nativeoidc

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/soulteary/yunqi/internal/redisconn"
)

const maxPendingFlows = 4096

var errUnavailable = errors.New("native identity unavailable")
var errInvalid = errors.New("invalid native identity")

type record struct {
	Value   string
	Expires int64
	Subject string
	Created int64
}
type storage struct {
	mu     sync.Mutex
	local  map[string]record
	redis  *redis.Client
	prefix string
}

func newStorage(rawURL, prefix string) (*storage, error) {
	s := &storage{local: make(map[string]record), prefix: prefix}
	if rawURL != "" {
		c, err := redisconn.New(rawURL)
		if err != nil {
			return nil, errUnavailable
		}
		s.redis = c
	}
	return s, nil
}
func (s *storage) close() error {
	if s.redis != nil {
		return s.redis.Close()
	}
	return nil
}
func (s *storage) put(ctx context.Context, key, value, subject string, expires int64) error {
	now := time.Now().Unix()
	if expires <= now || (subject == "" && expires-now > 300) {
		return errInvalid
	}
	if s.redis != nil {
		// Atomically bound each subject to twenty sessions, evicting the oldest.
		// Session keys and subject index share one hash slot. There is no immortal
		// browser credential to resurrect when Redis session state disappears.
		script := redis.NewScript(`
local k=KEYS[1]
local idx=KEYS[2]
if ARGV[4] ~= '' then
 local members=redis.call('ZRANGE',idx,0,-1)
 for _,v in ipairs(members) do if redis.call('EXISTS',v)==0 then redis.call('ZREM',idx,v) end end
 local rate=KEYS[3]
 local issued=redis.call('INCR',rate)
 if issued == 1 then redis.call('EXPIRE',rate,3600) end
 if issued > 100 then return 0 end
 local clock=redis.call('TIME')
 redis.call('ZADD',idx,tonumber(clock[1])*1000000+tonumber(clock[2]),k)
 redis.call('EXPIRE',idx,86400)
 local old=redis.call('ZRANGE',idx,0,-21)
 for _,v in ipairs(old) do redis.call('DEL',v);redis.call('ZREM',idx,v) end
else
 local clock=redis.call('TIME')
 local now=tonumber(clock[1])+tonumber(clock[2])/1000000
 redis.call('ZREMRANGEBYSCORE',idx,'-inf',now)
 if redis.call('ZCARD',idx) >= tonumber(ARGV[3]) then return 0 end
 redis.call('ZADD',idx,now+tonumber(ARGV[2])+1,k)
 redis.call('EXPIRE',idx,302)
end
redis.call('SET',k,ARGV[1],'EX',ARGV[2])
return 1`)
		slot := subject
		if slot == "" {
			slot = "flows"
		}
		base := s.prefix + "{" + slot + "}:"
		result, err := script.Run(ctx, s.redis, []string{base + key, base + "sessions", base + "issued"}, value, expires-now, maxPendingFlows, subject).Int()
		if err != nil || result != 1 {
			return errUnavailable
		}
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.local {
		if v.Expires <= now {
			delete(s.local, k)
		}
	}
	if subject != "" {
		rateKey := "issued:" + subject
		rate := s.local[rateKey]
		issued, _ := strconv.Atoi(rate.Value)
		if issued >= 100 {
			return errUnavailable
		}
		if rate.Expires <= now {
			rate.Expires = now + 3600
		}
		rate.Value = strconv.Itoa(issued + 1)
		s.local[rateKey] = rate
		count := 0
		oldest := ""
		var oldestTime int64
		for k, v := range s.local {
			if v.Subject == subject {
				count++
				if oldest == "" || v.Created < oldestTime {
					oldest = k
					oldestTime = v.Created
				}
			}
		}
		if count >= 20 {
			delete(s.local, oldest)
		}
	}
	if len(s.local) >= maxPendingFlows {
		return errUnavailable
	}
	s.local[key] = record{Value: value, Expires: expires, Subject: subject, Created: time.Now().UnixNano()}
	return nil
}
func (s *storage) get(ctx context.Context, key, subject string, consume bool) (string, error) {
	if s.redis != nil {
		slot := subject
		if slot == "" {
			slot = "flows"
		}
		k := s.prefix + "{" + slot + "}:" + key
		var value string
		var err error
		if consume {
			value, err = redis.NewScript(`
local value=redis.call('GET',KEYS[1])
redis.call('DEL',KEYS[1])
redis.call('ZREM',KEYS[2],KEYS[1])
return value`).Run(ctx, s.redis, []string{k, s.prefix + "{" + slot + "}:sessions"}).Text()
		} else {
			value, err = s.redis.Get(ctx, k).Result()
		}
		if errors.Is(err, redis.Nil) {
			return "", errInvalid
		}
		if err != nil {
			return "", errUnavailable
		}
		return value, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.local[key]
	if consume {
		delete(s.local, key)
	}
	if !ok || v.Expires <= time.Now().Unix() {
		delete(s.local, key)
		return "", errInvalid
	}
	return v.Value, nil
}
func marshal(value interface{}) string { b, _ := json.Marshal(value); return string(b) }
