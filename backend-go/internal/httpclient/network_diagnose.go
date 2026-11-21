package httpclient

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/url"
	"time"

	"github.com/soulteary/yunqi/internal/utils"
)

type NetworkDiagnosis struct {
	Branch         string `json:"branch"`
	Stage          string `json:"stage"`
	Success        bool   `json:"success"`
	Error          string `json:"error,omitempty"`
	InterfaceName  string `json:"interfaceName"`
	InterfaceIndex int    `json:"interfaceIndex"`
	BindingApplied bool   `json:"bindingApplied"`
	LocalAddress   string `json:"localAddress,omitempty"`
	RemoteAddress  string `json:"remoteAddress,omitempty"`
	Fingerprint    string `json:"fingerprint,omitempty"`
}

// DiagnoseNetwork sends no HTTP request, credentials or inference body. Proxy
// branches inspect only the Yunqi-to-proxy socket; they do not claim upstream reachability.
func DiagnoseNetwork(ctx context.Context, baseURL string, opts ClientOptions) []NetworkDiagnosis {
	p, err := ResolveOutboundPlan(opts)
	if err != nil {
		result := NetworkDiagnosis{Stage: "config"}
		setNetworkDiagnosisError(&result, err)
		return []NetworkDiagnosis{result}
	}
	target, err := url.Parse(baseURL)
	if err != nil {
		return []NetworkDiagnosis{{Stage: "config", Error: "invalid base URL"}}
	}
	branches := []string{"direct"}
	if p.ProxyURL != "" {
		if p.PreferDirect {
			branches = append(branches, "proxy")
		} else {
			branches = []string{"proxy"}
		}
	}
	results := make([]NetworkDiagnosis, 0, len(branches))
	for _, branch := range branches {
		result := NetworkDiagnosis{Branch: branch, Stage: "dns", InterfaceName: p.Config.InterfaceName, InterfaceIndex: p.InterfaceIndex, Fingerprint: p.Fingerprint}
		endpoint := target
		if branch == "proxy" {
			endpoint, _ = url.Parse(p.ProxyURL)
		}
		if endpoint == nil || utils.IsMetadataHost(endpoint.Hostname()) {
			result.Error = "blocked destination"
			results = append(results, result)
			continue
		}
		branchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		ips, e := p.LookupIPAddr(branchCtx, endpoint.Hostname())
		if e == nil {
			for _, ip := range ips {
				if utils.IsMetadataIP(ip.IP) {
					e = errMetadataDestination
					break
				}
			}
		}
		if e != nil {
			result.Error = "DNS lookup failed or destination blocked"
			cancel()
			results = append(results, result)
			continue
		}
		port := endpoint.Port()
		if port == "" {
			switch endpoint.Scheme {
			case "https":
				port = "443"
			case "socks5", "socks5h":
				port = "1080"
			default:
				port = "80"
			}
		}
		result.Stage = "tcp"
		conn, e := p.DialContext(branchCtx, "tcp", net.JoinHostPort(endpoint.Hostname(), port))
		if e == nil {
			result.LocalAddress = conn.LocalAddr().String()
			result.RemoteAddress = conn.RemoteAddr().String()
			result.BindingApplied = p.InterfaceIndex != 0
			if endpoint.Scheme == "https" {
				result.Stage = "tls"
				secure := tls.Client(conn, &tls.Config{ServerName: endpoint.Hostname(), MinVersion: tls.VersionTLS12, InsecureSkipVerify: opts.Insecure})
				e = secure.HandshakeContext(branchCtx)
			}
			conn.Close()
		}
		if e != nil {
			setNetworkDiagnosisError(&result, e)
		} else {
			result.Success = true
			result.Stage = "complete"
		}
		cancel()
		results = append(results, result)
	}
	return results
}

func setNetworkDiagnosisError(result *NetworkDiagnosis, err error) {
	var local *EgressError
	if errors.As(err, &local) {
		result.Stage = local.Stage
		result.Error = local.Error()
		return
	}
	result.Error = "Connection validation failed"
}
