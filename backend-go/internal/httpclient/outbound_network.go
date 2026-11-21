package httpclient

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"golang.org/x/net/proxy"
)

// EgressError identifies local policy failures; it must not penalize upstream keys.
type EgressError struct {
	Stage string
	Err   error
}

func (e *EgressError) Error() string {
	return "network_egress_unavailable (" + e.Stage + "): " + e.Err.Error()
}
func (e *EgressError) Unwrap() error { return e.Err }
func IsEgressError(err error) bool   { var e *EgressError; return errors.As(err, &e) }
func egressError(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &EgressError{stage, err}
}

type errorTransport struct{ err error }

func (t errorTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		r.Body.Close()
	}
	return nil, t.err
}

// OutboundPlan is an immutable request policy snapshot. Resolving it never dials.
type OutboundPlan struct {
	Config         config.OutboundNetworkConfig
	ProxyURL       string
	PreferDirect   bool
	InterfaceIndex int
	Fingerprint    string
	dialer         *net.Dialer
}

func ResolveOutboundPlan(opts ClientOptions) (*OutboundPlan, error) {
	c := config.CloneOutboundNetwork(opts.OutboundNetwork)
	if c == nil {
		c = &config.OutboundNetworkConfig{}
	}
	if err := config.ValidateOutboundNetwork(c, opts.ProxyURL); err != nil {
		return nil, egressError("config", err)
	}
	p := &OutboundPlan{Config: *c, ProxyURL: opts.ProxyURL, PreferDirect: opts.ProxyPreferDirect}
	switch c.ProxyMode {
	case "direct":
		p.ProxyURL = ""
		p.PreferDirect = false
	case "proxy":
		p.PreferDirect = false
	case "direct_first":
		p.PreferDirect = true
	}
	var iface *net.Interface
	var addresses []string
	if c.InterfaceName != "" {
		if !interfaceBindingSupported {
			return nil, egressError("interface", errors.New("interface binding unsupported on this platform"))
		}
		var err error
		iface, err = net.InterfaceByName(c.InterfaceName)
		if err != nil {
			return nil, egressError("interface", errors.New("configured interface is unavailable"))
		}
		if iface.Flags&net.FlagUp == 0 {
			return nil, egressError("interface", errors.New("configured interface is down"))
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, egressError("interface", err)
		}
		has4, has6 := false, false
		for _, a := range addrs {
			addresses = append(addresses, a.String())
			ip, _, e := net.ParseCIDR(a.String())
			if e == nil {
				if ip.To4() != nil {
					has4 = true
				} else {
					has6 = true
				}
			}
		}
		if (!has4 && !has6) || (c.AddressFamily == "ipv4" && !has4) || (c.AddressFamily == "ipv6" && !has6) {
			return nil, egressError("interface", errors.New("interface has no address for requested family"))
		}
		p.InterfaceIndex = iface.Index
	}
	sort.Strings(addresses)
	raw, _ := json.Marshal(struct {
		Config    config.OutboundNetworkConfig
		Proxy     string
		Prefer    bool
		Index     int
		Addresses []string
	}{*c, p.ProxyURL, p.PreferDirect, p.InterfaceIndex, addresses})
	p.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(raw))
	d := newMetadataSafeDialer()
	guard := d.ControlContext
	d.ControlContext = func(ctx context.Context, network, addr string, raw syscall.RawConn) error {
		if err := guard(ctx, network, addr, raw); err != nil {
			return err
		}
		if iface != nil {
			current, err := net.InterfaceByName(iface.Name)
			if err != nil || current.Index != iface.Index || current.Flags&net.FlagUp == 0 {
				return egressError("interface", errors.New("configured interface changed"))
			}
			if err := bindSocket(raw, network, iface); err != nil {
				return egressError("bind", err)
			}
		}
		return nil
	}
	if c.DNS.Mode == "bound" {
		var next atomic.Uint64
		dnsDialer := *d // No Resolver on this copy: DNS server is always a numeric endpoint.
		d.Resolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var last error
			start := int(next.Add(1)-1) % len(c.DNS.Servers)
			for i := range c.DNS.Servers {
				server := c.DNS.Servers[(start+i)%len(c.DNS.Servers)]
				conn, err := dnsDialer.DialContext(ctx, network, server)
				if err == nil {
					return conn, nil
				}
				last = err
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
			}
			return nil, egressError("dns", last)
		}}
	}
	p.dialer = d
	return p, nil
}
func (p *OutboundPlan) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if p.Config.AddressFamily == "ipv4" {
		network = strings.TrimRight(network, "46") + "4"
	}
	if p.Config.AddressFamily == "ipv6" {
		network = strings.TrimRight(network, "46") + "6"
	}
	conn, err := p.dialer.DialContext(ctx, network, addr)
	if err != nil && ctx.Err() == nil && p.Config.AddressFamily != "" && p.Config.AddressFamily != "auto" && strings.Contains(err.Error(), "no suitable address") {
		return nil, egressError("address_family", err)
	}
	if err != nil && ctx.Err() == nil && p.Config.DNS.Mode == "bound" {
		var de *net.DNSError
		if errors.As(err, &de) {
			return nil, egressError("dns", err)
		}
	}
	return conn, err
}
func (p *OutboundPlan) Dial(network, addr string) (net.Conn, error) {
	return p.DialContext(context.Background(), network, addr)
}
func (p *OutboundPlan) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	resolver := p.dialer.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil && p.Config.DNS.Mode == "bound" && ctx.Err() == nil {
		return nil, egressError("dns", err)
	}
	return ips, err
}
func (p *OutboundPlan) transport(opts ClientOptions, proxied bool) (http.RoundTripper, error) {
	t := buildTransport(opts.Stream, opts.ResponseHeaderTimeout, opts.Insecure)
	t.DialContext = p.DialContext
	if proxied {
		u, err := url.Parse(p.ProxyURL)
		if err != nil {
			return nil, errors.New("invalid proxy URL")
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https":
			t.Proxy = http.ProxyURL(u)
		case "socks5", "socks5h":
			var auth *proxy.Auth
			if u.User != nil {
				password, _ := u.User.Password()
				auth = &proxy.Auth{User: u.User.Username(), Password: password}
			}
			d, err := proxy.SOCKS5("tcp", u.Host, auth, p)
			if err != nil {
				return nil, err
			}
			cd, ok := d.(proxy.ContextDialer)
			if !ok {
				return nil, errors.New("SOCKS dialer does not support cancellation")
			}
			t.DialContext = cd.DialContext
			t.ForceAttemptHTTP2 = false
		default:
			return nil, errors.New("unsupported proxy protocol")
		}
	}
	guard := &metadataGuardTransport{base: t, privateLookup: p.LookupIPAddr}
	if proxied {
		guard.lookup = p.LookupIPAddr
	}
	return guard, nil
}
func (cm *ClientManager) outboundClient(opts ClientOptions, useCache bool) *http.Client {
	opts.OutboundNetwork = config.CloneOutboundNetwork(opts.OutboundNetwork)
	p, err := ResolveOutboundPlan(opts)
	if err != nil {
		return &http.Client{Transport: errorTransport{err}}
	}
	opts.ResponseHeaderTimeout = normalizeResponseHeaderTimeout(opts.ResponseHeaderTimeout)
	if opts.Stream {
		opts.Timeout = 0
	}
	key := fmt.Sprintf("egress-%s-%t-%d-%d-%t", p.Fingerprint, opts.Stream, opts.Timeout, opts.ResponseHeaderTimeout, opts.Insecure)
	if useCache {
		cm.mu.Lock()
		defer cm.mu.Unlock()
		if c := cm.clients[key]; c != nil {
			return c
		}
	}
	direct, err := p.transport(opts, false)
	if err != nil {
		return &http.Client{Transport: errorTransport{egressError("config", err)}}
	}
	var rt http.RoundTripper = direct
	if p.ProxyURL != "" {
		proxied, e := p.transport(opts, true)
		if e != nil {
			return &http.Client{Transport: errorTransport{egressError("proxy", e)}}
		}
		if p.PreferDirect {
			rt = &directFirstRoundTripper{direct: direct, proxied: proxied, proxyURL: p.ProxyURL}
		} else {
			rt = proxied
		}
	}
	// Revalidate interface on every request, even if a pooled socket is available.
	rt = &policyTransport{base: rt, opts: opts, fingerprint: p.Fingerprint}
	client := &http.Client{Transport: rt, Timeout: opts.Timeout}
	if useCache {
		// Bounded cache; eviction closes idle connections only, never active SSE.
		if len(cm.clients) >= 256 {
			for k, old := range cm.clients {
				old.CloseIdleConnections()
				delete(cm.clients, k)
			}
		}
		cm.clients[key] = client
	}
	return client
}

type policyTransport struct {
	base        http.RoundTripper
	opts        ClientOptions
	fingerprint string
}

func (t *policyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	p, err := ResolveOutboundPlan(t.opts)
	if err == nil && p.Fingerprint != t.fingerprint {
		err = egressError("interface", errors.New("interface snapshot changed; retry with a new client"))
	}
	if err != nil {
		if r.Body != nil {
			r.Body.Close()
		}
		return nil, err
	}
	return t.base.RoundTrip(r)
}
func (t *policyTransport) CloseIdleConnections() {
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

// NetworkInterface reports only the current process's network namespace.
type NetworkInterface struct {
	Name             string   `json:"name"`
	Index            int      `json:"index"`
	Addresses        []string `json:"addresses"`
	Up               bool     `json:"up"`
	Loopback         bool     `json:"loopback"`
	BindingSupported bool     `json:"bindingSupported"`
}

func ListNetworkInterfaces() ([]NetworkInterface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]NetworkInterface, 0, len(interfaces))
	for _, i := range interfaces {
		v := NetworkInterface{Name: i.Name, Index: i.Index, Up: i.Flags&net.FlagUp != 0, Loopback: i.Flags&net.FlagLoopback != 0, BindingSupported: interfaceBindingSupported, Addresses: []string{}}
		as, e := i.Addrs()
		if e != nil {
			return nil, e
		}
		for _, a := range as {
			v.Addresses = append(v.Addresses, a.String())
		}
		out = append(out, v)
	}
	return out, nil
}

// WithOutboundContext carries channel policy through Copilot token/catalog calls.
// Account-level OAuth without a channel deliberately retains its own defaults.
type outboundContextKey struct{}

func WithOutboundContext(ctx context.Context, u *config.UpstreamConfig) context.Context {
	if u == nil {
		return ctx
	}
	return context.WithValue(ctx, outboundContextKey{}, ClientOptions{Timeout: 30 * time.Second, ProxyURL: u.ProxyURL, ProxyPreferDirect: u.ProxyPreferDirect, Insecure: u.InsecureSkipVerify, OutboundNetwork: config.CloneOutboundNetwork(u.OutboundNetwork)})
}
func OutboundOptionsFromContext(ctx context.Context) (ClientOptions, bool) {
	v, ok := ctx.Value(outboundContextKey{}).(ClientOptions)
	return v, ok
}
