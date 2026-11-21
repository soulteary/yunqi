package httpclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"syscall"
	"time"

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
			if utils.IsMetadataHost(host) {
				return errMetadataDestination
			}
			return nil
		},
	}
}

type metadataGuardTransport struct {
	base   http.RoundTripper
	lookup metadataLookup
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
	if req == nil || req.URL == nil {
		return nil, errors.New("missing request URL")
	}
	host := req.URL.Hostname()
	if utils.IsMetadataHost(host) {
		return nil, errMetadataDestination
	}
	if t.lookup != nil {
		ips, err := t.lookup(req.Context(), host)
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		if IsEgressError(err) {
			return nil, err
		}
		if err == nil {
			for _, ip := range ips {
				if utils.IsMetadataIP(ip.IP) {
					return nil, errMetadataDestination
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
