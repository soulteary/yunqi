package httpclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/soulteary/yunqi/internal/redaction"
	"github.com/soulteary/yunqi/internal/utils"
)

var errMetadataDestination = errors.New("cloud metadata destination is blocked")

type metadataLookup func(context.Context, string) ([]net.IPAddr, error)

// The standard Dialer retains Happy Eyeballs, context cancellation and its
// shared timeout budget. ControlContext receives the actual resolved socket
// address, so a DNS change after URL validation cannot select metadata.
func newMetadataSafeDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		ControlContext: func(_ context.Context, _, addr string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return err
			}
			policy, err := loadOutboundPolicy()
			if err != nil {
				return outboundPolicyError(err)
			}
			if utils.IsMetadataHost(host) {
				return errMetadataDestination
			}
			if err := policy.validateIP(net.ParseIP(strings.Split(host, "%")[0])); err != nil {
				return outboundPolicyError(err)
			}
			return nil
		},
	}
}

type metadataGuardTransport struct {
	base          http.RoundTripper
	lookup        metadataLookup
	privateLookup metadataLookup
}

func protectMetadataTransport(base http.RoundTripper, proxied bool) http.RoundTripper {
	t := &metadataGuardTransport{base: base}
	if proxied {
		t.lookup = net.DefaultResolver.LookupIPAddr
	}
	return t
}

// RoundTrip runs for the initial request and each redirect, including requests
// using an existing pooled connection. Proxies may resolve names remotely: local
// DNS failures remain allowed, but known aliases and locally observed metadata
// addresses are rejected before any request is sent to the proxy.
func (t *metadataGuardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	redaction.ObserveRequest(req)
	if req == nil || req.URL == nil {
		return blockedOutboundRequest(req, errors.New("missing request URL"))
	}
	policy, err := loadOutboundPolicy()
	if err != nil {
		return blockedOutboundRequest(req, outboundPolicyError(err))
	}
	if err := policy.validateURL(req.URL); err != nil {
		return blockedOutboundRequest(req, outboundPolicyError(err))
	}
	host := req.URL.Hostname()
	lookup := t.lookup
	if lookup == nil && policy.blockPrivate {
		lookup = t.privateLookup
		if lookup == nil {
			lookup = net.DefaultResolver.LookupIPAddr
		}
	}
	if lookup != nil {
		ips, err := lookup(req.Context(), host)
		if req.Context().Err() != nil {
			return blockedOutboundRequest(req, req.Context().Err())
		}
		if IsEgressError(err) {
			return blockedOutboundRequest(req, err)
		}
		if err != nil && policy.blockPrivate {
			return blockedOutboundRequest(req, outboundPolicyError(errors.New("cannot resolve restricted outbound destination")))
		}
		if err == nil {
			for _, ip := range ips {
				if err := policy.validateIP(ip.IP); err != nil {
					return blockedOutboundRequest(req, outboundPolicyError(err))
				}
			}
		}
	}
	return t.base.RoundTrip(req)
}

func (t *metadataGuardTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func blockedOutboundRequest(req *http.Request, err error) (*http.Response, error) {
	if req != nil && req.Body != nil {
		_ = req.Body.Close()
	}
	return nil, err
}
