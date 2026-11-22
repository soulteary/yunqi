// Package redisconn creates bounded Redis clients under the gateway's global
// outbound policy. Credentials never appear in its public errors.
package redisconn

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/url"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/soulteary/yunqi/internal/httpclient"
)

var ErrConfiguration = errors.New("invalid Redis connection configuration")

func New(rawURL string) (*redis.Client, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed == nil || (parsed.Scheme != "redis" && parsed.Scheme != "rediss") || parsed.Hostname() == "" {
		return nil, ErrConfiguration
	}
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, ErrConfiguration
	}
	options.DialTimeout = 2 * time.Second
	options.ReadTimeout = 2 * time.Second
	options.WriteTimeout = 2 * time.Second
	options.ContextTimeoutEnabled = true
	options.MaxRetries = -1
	options.DialerRetries = 1
	options.PoolSize = 16
	options.PoolTimeout = 2 * time.Second
	options.DisableIdentity = true
	if options.TLSConfig != nil {
		options.TLSConfig.MinVersion = tls.VersionTLS12
	}
	options.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
		call, cancel := context.WithTimeout(ctx, options.DialTimeout)
		defer cancel()
		conn, err := httpclient.DialOutboundTCPContext(call, network, addr)
		if err != nil {
			return nil, err
		}
		if options.TLSConfig != nil {
			secured := tls.Client(conn, options.TLSConfig.Clone())
			if err := secured.HandshakeContext(call); err != nil {
				_ = conn.Close()
				return nil, err
			}
			return secured, nil
		}
		return conn, nil
	}
	client := redis.NewClient(options)
	client.AddHook(&policyHook{network: options.Network, address: options.Addr})
	return client, nil
}

type policyHook struct{ network, address string }

func (h *policyHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *policyHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if err := httpclient.ValidateOutboundTCPAddress(ctx, h.network, h.address); err != nil {
			return err
		}
		return next(ctx, cmd)
	}
}
func (h *policyHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if err := httpclient.ValidateOutboundTCPAddress(ctx, h.network, h.address); err != nil {
			return err
		}
		return next(ctx, cmds)
	}
}
