package entrylease

import "github.com/redis/go-redis/v9"

// All keys use one principal hash tag. Redis TIME is authoritative, so node
// clock skew cannot extend leases or reset RPM windows. Accepted RPM entries
// remain after release; retries inside one logical request do not re-acquire.
var acquireScript = redis.NewScript(`
local clock=redis.call('TIME')
local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
local id=ARGV[1]
local ttl=tonumber(ARGV[2])
local concurrent=tonumber(ARGV[3])
local rpm=tonumber(ARGV[4])
redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',now)
redis.call('ZREMRANGEBYSCORE',KEYS[2],'-inf',now-60000)
if redis.call('ZSCORE',KEYS[1],id) then return {1,0} end
if concurrent>0 and redis.call('ZCARD',KEYS[1])>=concurrent then
 local first=redis.call('ZRANGE',KEYS[1],0,0,'WITHSCORES')
 return {0,math.max(1,tonumber(first[2])-now)}
end
if rpm>0 and redis.call('ZCARD',KEYS[2])>=rpm then
 local first=redis.call('ZRANGE',KEYS[2],0,0,'WITHSCORES')
 return {2,math.max(1,tonumber(first[2])+60000-now)}
end
redis.call('ZADD',KEYS[1],now+ttl,id)
local latest=redis.call('ZREVRANGE',KEYS[1],0,0,'WITHSCORES')
redis.call('PEXPIREAT',KEYS[1],tonumber(latest[2])+1000)
if rpm>0 then
 redis.call('ZADD',KEYS[2],now,id)
 redis.call('PEXPIRE',KEYS[2],60000+ttl)
end
return {1,0}
`)
var renewScript = redis.NewScript(`
local clock=redis.call('TIME')
local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',now)
if not redis.call('ZSCORE',KEYS[1],ARGV[1]) then return 0 end
redis.call('ZADD',KEYS[1],'XX',now+tonumber(ARGV[2]),ARGV[1])
local latest=redis.call('ZREVRANGE',KEYS[1],0,0,'WITHSCORES')
redis.call('PEXPIREAT',KEYS[1],tonumber(latest[2])+1000)
return 1
`)
var releaseScript = redis.NewScript(`return redis.call('ZREM',KEYS[1],ARGV[1])`)
