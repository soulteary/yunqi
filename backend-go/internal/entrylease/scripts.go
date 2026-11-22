package entrylease

import "github.com/redis/go-redis/v9"

// Keys share a principal or routing-realm hash tag. Redis TIME is authoritative, so node
// clock skew cannot extend leases or reset RPM windows. Accepted RPM entries
// remain after release; retries inside one logical request do not re-acquire.
var acquireScript = redis.NewScript(`
if #KEYS>=3 and redis.call('GET',KEYS[3])~=ARGV[6] then return redis.error_reply('routing epoch unavailable') end
local clock=redis.call('TIME')
local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
local id=ARGV[1]
local ttl=tonumber(ARGV[2])
local concurrent=tonumber(ARGV[3])
local rpm=tonumber(ARGV[4])
local window=tonumber(ARGV[5]) or 60000
redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',now)
redis.call('ZREMRANGEBYSCORE',KEYS[2],'-inf',now-window)
if redis.call('ZSCORE',KEYS[1],id) then return {1,0} end
if concurrent>0 and redis.call('ZCARD',KEYS[1])>=concurrent then
 local first=redis.call('ZRANGE',KEYS[1],0,0,'WITHSCORES')
 return {0,math.max(1,tonumber(first[2])-now)}
end
if rpm>0 and redis.call('ZCARD',KEYS[2])>=rpm then
 local first=redis.call('ZRANGE',KEYS[2],0,0,'WITHSCORES')
 return {2,math.max(1,tonumber(first[2])+window-now)}
end
redis.call('ZADD',KEYS[1],now+ttl,id)
local latest=redis.call('ZREVRANGE',KEYS[1],0,0,'WITHSCORES')
redis.call('PEXPIREAT',KEYS[1],tonumber(latest[2])+1000)
if rpm>0 then
 redis.call('ZADD',KEYS[2],now,id)
 redis.call('PEXPIRE',KEYS[2],window+ttl)
end
return {1,0}
`)
var renewScript = redis.NewScript(`
if #KEYS>=3 and redis.call('GET',KEYS[3])~=ARGV[6] then return redis.error_reply('routing epoch unavailable') end
if #KEYS>=4 and ARGV[3]~='' then
 if redis.call('GET',KEYS[4])~=ARGV[3] then return 0 end
end
local clock=redis.call('TIME')
local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',now)
if not redis.call('ZSCORE',KEYS[1],ARGV[1]) then return 0 end
if #KEYS>=5 and ARGV[3]~='' then
 redis.call('PEXPIRE',KEYS[4],ARGV[4])
 redis.call('ZADD',KEYS[5],now+tonumber(ARGV[4]),KEYS[4])
end
redis.call('ZADD',KEYS[1],'XX',now+tonumber(ARGV[2]),ARGV[1])
local latest=redis.call('ZREVRANGE',KEYS[1],0,0,'WITHSCORES')
redis.call('PEXPIREAT',KEYS[1],tonumber(latest[2])+1000)
return 1
`)
var releaseScript = redis.NewScript(`return redis.call('ZREM',KEYS[1],ARGV[1])`)

// Routing state shares a Redis hash tag with its durable epoch. A missing epoch
// never creates a new quota or affinity realm during authentication or startup.
var strictBindingScript = redis.NewScript(`
if redis.call('GET',KEYS[3])~=ARGV[6] then return redis.error_reply('routing epoch unavailable') end
local clock=redis.call('TIME')
local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
local expiry=tonumber(redis.call('ZSCORE',KEYS[1],ARGV[1]) or '0')
if expiry<=now then return redis.error_reply('routing lease unavailable') end
local current=redis.call('GET',KEYS[4])
-- A live reservation without its binding is lost state, not an unused session.
-- Never allow a different resource to replace encrypted continuation affinity.
if not current and tonumber(redis.call('ZSCORE',KEYS[5],KEYS[4]) or '0')>now then
 return redis.error_reply('strict session binding lost')
end
if not current and ARGV[3]~='' then
 redis.call('ZREMRANGEBYSCORE',KEYS[5],'-inf',now)
 if redis.call('ZCARD',KEYS[5])>=4096 then return redis.error_reply('strict session capacity exceeded') end
 redis.call('SET',KEYS[4],ARGV[3],'PX',ARGV[4],'NX')
 current=redis.call('GET',KEYS[4])
end
if current then
 redis.call('PEXPIRE',KEYS[4],ARGV[4])
 redis.call('ZADD',KEYS[5],now+tonumber(ARGV[4]),KEYS[4])
end
return current or ''
`)
