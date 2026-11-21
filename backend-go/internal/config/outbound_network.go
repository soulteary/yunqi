package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// OutboundNetworkConfig is shared by all physical routes of a logical channel.
// Interface availability is deliberately checked at request time, not at load time.
type OutboundNetworkConfig struct {
	ProxyMode     string            `json:"proxyMode,omitempty"`
	InterfaceName string            `json:"interfaceName,omitempty"`
	AddressFamily string            `json:"addressFamily,omitempty"`
	DNS           OutboundDNSConfig `json:"dns,omitempty"`
}
type OutboundDNSConfig struct {
	Mode    string   `json:"mode,omitempty"`
	Servers []string `json:"servers,omitempty"`
}

// OutboundNetworkPatch distinguishes omission from explicit JSON null.
type OutboundNetworkPatch struct {
	Set   bool
	Value *OutboundNetworkConfig
}

func (p *OutboundNetworkPatch) UnmarshalJSON(b []byte) error {
	p.Set = true
	return json.Unmarshal(b, &p.Value)
}
func (p OutboundNetworkPatch) IsZero() bool                 { return !p.Set }
func (p OutboundNetworkPatch) MarshalJSON() ([]byte, error) { return json.Marshal(p.Value) }
func NetworkPatch(c *OutboundNetworkConfig) OutboundNetworkPatch {
	return OutboundNetworkPatch{Set: true, Value: CloneOutboundNetwork(c)}
}
func CloneOutboundNetwork(c *OutboundNetworkConfig) *OutboundNetworkConfig {
	if c == nil {
		return nil
	}
	v := *c
	v.DNS.Servers = append([]string(nil), c.DNS.Servers...)
	return &v
}
func ValidateOutboundNetwork(c *OutboundNetworkConfig, proxyURL string) error {
	if c == nil {
		return nil
	}
	switch c.ProxyMode {
	case "", "legacy", "direct":
	case "proxy", "direct_first":
		u, e := url.Parse(proxyURL)
		if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") || u.Fragment != "" {
			return fmt.Errorf("outboundNetwork: 有效的 HTTP/HTTPS/SOCKS5 代理地址必填")
		}
	default:
		return fmt.Errorf("outboundNetwork: 无效的代理模式")
	}
	switch c.AddressFamily {
	case "", "auto", "ipv4", "ipv6":
	default:
		return fmt.Errorf("outboundNetwork: 无效的地址族")
	}
	if len(c.InterfaceName) > 63 || strings.ContainsAny(c.InterfaceName, " /\\\x00\r\n\t") {
		return fmt.Errorf("outboundNetwork: 无效的接口名称")
	}
	switch c.DNS.Mode {
	case "", "system":
		if c.InterfaceName != "" {
			return fmt.Errorf("outboundNetwork: 指定网卡须使用 bound DNS")
		}
		if len(c.DNS.Servers) > 0 {
			return fmt.Errorf("outboundNetwork: system DNS 不接受服务器列表")
		}
	case "bound":
		if len(c.DNS.Servers) == 0 || len(c.DNS.Servers) > 4 {
			return fmt.Errorf("outboundNetwork: DNS 服务器数量须为 1–4")
		}
	default:
		return fmt.Errorf("outboundNetwork: 无效的 DNS 模式")
	}
	for _, s := range c.DNS.Servers {
		host, port, e := net.SplitHostPort(s)
		ip, ie := netip.ParseAddr(host)
		n, pe := strconv.Atoi(port)
		if e != nil || ie != nil || pe != nil || n < 1 || n > 65535 || ip.IsUnspecified() || ip.IsMulticast() {
			return fmt.Errorf("outboundNetwork: DNS 须为 IP:端口")
		}
		if c.AddressFamily == "ipv4" && !ip.Is4() || c.AddressFamily == "ipv6" && !ip.Is6() {
			return fmt.Errorf("outboundNetwork: DNS 服务器与地址族不匹配")
		}
		if ip.Zone() != "" && (c.InterfaceName == "" || ip.Zone() != c.InterfaceName) {
			return fmt.Errorf("outboundNetwork: IPv6 zone 须与网卡一致")
		}
		if ip.Is6() && ip.IsLinkLocalUnicast() && ip.Zone() == "" {
			return fmt.Errorf("outboundNetwork: IPv6 链路本地 DNS 须指定 zone")
		}
	}
	return nil
}
