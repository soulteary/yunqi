package httpclient

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/soulteary/yunqi/internal/utils"
)

type outboundPolicy struct {
	hosts        []string
	ports        map[string]bool
	blockPrivate bool
	privateCIDRs []netip.Prefix
}

// Empty restrictions preserve private deployments. Metadata is never exempted.
// Environment settings are checked on each request and actual dial so pooled
// connections and cached clients cannot bypass a policy tightened at runtime.
func loadOutboundPolicy() (outboundPolicy, error) {
	p := outboundPolicy{hosts: splitPolicyList(os.Getenv("YUNQI_OUTBOUND_ALLOWED_HOSTS")), ports: map[string]bool{}}
	for _, port := range splitPolicyList(os.Getenv("YUNQI_OUTBOUND_ALLOWED_PORTS")) {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return p, errors.New("invalid outbound allowed port")
		}
		p.ports[strconv.Itoa(n)] = true
	}
	if raw := os.Getenv("YUNQI_OUTBOUND_BLOCK_PRIVATE"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return p, errors.New("invalid outbound private-address policy")
		}
		p.blockPrivate = v
	}
	for _, raw := range splitPolicyList(os.Getenv("YUNQI_OUTBOUND_PRIVATE_CIDRS")) {
		cidr, err := netip.ParsePrefix(raw)
		if err != nil {
			return p, errors.New("invalid outbound private CIDR")
		}
		p.privateCIDRs = append(p.privateCIDRs, cidr.Masked())
	}
	return p, nil
}
func splitPolicyList(raw string) []string {
	var result []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.ToLower(strings.TrimSpace(part)); part != "" {
			result = append(result, part)
		}
	}
	return result
}
func (p outboundPolicy) validateURL(u *url.URL) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return errors.New("outbound URL requires an HTTP(S) host")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if utils.IsMetadataHost(host) {
		return errMetadataDestination
	}
	if len(p.hosts) > 0 {
		allowed := false
		for _, pattern := range p.hosts {
			if host == pattern || (strings.HasPrefix(pattern, "*.") && strings.HasSuffix(host, pattern[1:]) && len(host) > len(pattern)-1) {
				allowed = true
				break
			}
		}
		if !allowed {
			return errors.New("outbound host is not allowed")
		}
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	if len(p.ports) > 0 && !p.ports[port] {
		return errors.New("outbound port is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		return p.validateIP(ip)
	}
	return nil
}
func (p outboundPolicy) validateIP(ip net.IP) error {
	if utils.IsMetadataIP(ip) {
		return errMetadataDestination
	}
	if !p.blockPrivate {
		return nil
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return errors.New("invalid outbound address")
	}
	addr = addr.Unmap()
	if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		for _, cidr := range p.privateCIDRs {
			if cidr.Contains(addr) {
				return nil
			}
		}
		return errors.New("private outbound address is not allowed")
	}
	return nil
}

// DefaultOutboundTransport applies the same policy to all standard-library
// clients used by model/price sync, console probes, downloads and notifications.
// A cloned transport retains proxy/environment/TLS defaults without mutating it.
func DefaultOutboundTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if transport, ok := base.(*http.Transport); ok {
		clone := transport.Clone()
		clone.DialContext = newMetadataSafeDialer().DialContext
		return protectMetadataTransport(clone, true)
	}
	return protectMetadataTransport(base, true)
}

var installOutboundPolicy sync.Once

func InstallDefaultOutboundPolicy() {
	installOutboundPolicy.Do(func() { http.DefaultTransport = DefaultOutboundTransport(http.DefaultTransport) })
}
func outboundPolicyError(err error) error {
	return egressError("policy", fmt.Errorf("outbound policy: %w", err))
}
