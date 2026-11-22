package httpclient

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ValidateOutboundTCPAddress applies the same host/port/private policy to
// non-HTTP connections, including operations on an already pooled connection.
func ValidateOutboundTCPAddress(ctx context.Context, network, addr string) error {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return outboundPolicyError(errors.New("outbound transport requires TCP"))
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return outboundPolicyError(errors.New("invalid outbound TCP address"))
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return outboundPolicyError(errors.New("invalid outbound TCP port"))
	}
	policy, err := loadOutboundPolicy()
	if err != nil {
		return outboundPolicyError(err)
	}
	if err := policy.validateURL(&url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(number))}); err != nil {
		return outboundPolicyError(err)
	}
	if policy.blockPrivate && net.ParseIP(strings.Split(host, "%")[0]) == nil {
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return outboundPolicyError(errors.New("cannot resolve restricted outbound destination"))
		}
		for _, address := range addresses {
			if err := policy.validateIP(address.IP); err != nil {
				return outboundPolicyError(err)
			}
		}
	}
	return nil
}

// DialOutboundTCPContext retains the standard resolver/Happy Eyeballs while
// ControlContext checks the actual selected IP, preventing DNS rebinding bypass.
func DialOutboundTCPContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if err := ValidateOutboundTCPAddress(ctx, network, addr); err != nil {
		return nil, err
	}
	return newMetadataSafeDialer().DialContext(ctx, network, addr)
}
