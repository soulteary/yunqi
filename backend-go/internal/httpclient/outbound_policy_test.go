package httpclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestOutboundPolicyDefaultsAndRestrictions(t *testing.T) {
	for _, test := range []struct {
		name, target, hosts, ports, cidrs string
		private, blocked                  bool
	}{
		{name: "private compatible", target: "http://127.0.0.1:11434"},
		{name: "ipv6 compatible", target: "http://[::1]:11434"},
		{name: "metadata blocked", target: "http://169.254.169.254", blocked: true},
		{name: "metadata never exempt", target: "http://[fd00:ec2::254]", private: true, cidrs: "::/0", blocked: true},
		{name: "allowed domain", target: "https://api.example.com", hosts: "*.example.com", ports: "443"},
		{name: "suffix boundary", target: "https://badexample.com", hosts: "*.example.com", blocked: true},
		{name: "port restricted", target: "http://api.example.com:8080", ports: "443", blocked: true},
		{name: "private restricted", target: "http://10.1.2.3", private: true, blocked: true},
		{name: "private exception", target: "http://10.1.2.3", private: true, cidrs: "10.0.0.0/8"},
		{name: "mapped IPv4", target: "http://[::ffff:127.0.0.1]", private: true, blocked: true},
		{name: "scheme", target: "file:///etc/passwd", blocked: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("YUNQI_OUTBOUND_ALLOWED_HOSTS", test.hosts)
			t.Setenv("YUNQI_OUTBOUND_ALLOWED_PORTS", test.ports)
			t.Setenv("YUNQI_OUTBOUND_PRIVATE_CIDRS", test.cidrs)
			private := "false"
			if test.private {
				private = "true"
			}
			t.Setenv("YUNQI_OUTBOUND_BLOCK_PRIVATE", private)
			policy, err := loadOutboundPolicy()
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(test.target)
			if err != nil {
				t.Fatal(err)
			}
			if blocked := policy.validateURL(u) != nil; blocked != test.blocked {
				t.Fatalf("blocked=%v expected=%v", blocked, test.blocked)
			}
		})
	}
}

func TestOutboundPolicyRejectsInvalidRestrictions(t *testing.T) {
	for key, value := range map[string]string{"YUNQI_OUTBOUND_ALLOWED_PORTS": "invalid", "YUNQI_OUTBOUND_BLOCK_PRIVATE": "invalid", "YUNQI_OUTBOUND_PRIVATE_CIDRS": "invalid"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			if _, err := loadOutboundPolicy(); err == nil {
				t.Fatal("invalid restriction was ignored")
			}
		})
	}
}

type policyCaptureTransport struct{ calls int }

func (t *policyCaptureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.calls++
	return nil, errors.New("captured")
}

func TestOutboundPolicyProtectsProxiedDNSAndPooledRequests(t *testing.T) {
	t.Setenv("YUNQI_OUTBOUND_BLOCK_PRIVATE", "true")
	capture := &policyCaptureTransport{}
	transport := &metadataGuardTransport{base: capture, lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}, nil
	}}
	req, _ := http.NewRequest(http.MethodGet, "https://allowed.example", nil)
	if _, err := transport.RoundTrip(req); err == nil || !IsEgressError(err) || capture.calls != 0 {
		t.Fatalf("restricted DNS request reached proxy: %v calls=%d", err, capture.calls)
	}
	t.Setenv("YUNQI_OUTBOUND_PRIVATE_CIDRS", "10.0.0.0/8")
	_, _ = transport.RoundTrip(req)
	if capture.calls != 1 {
		t.Fatal("explicit private exception was ignored")
	}
	t.Setenv("YUNQI_OUTBOUND_ALLOWED_HOSTS", "different.example")
	_, _ = transport.RoundTrip(req)
	if capture.calls != 1 {
		t.Fatal("cached transport bypassed tightened policy")
	}
}

func TestDefaultOutboundTransportRedirectAndPrivateCompatibility(t *testing.T) {
	t.Setenv("YUNQI_OUTBOUND_ALLOWED_HOSTS", "")
	t.Setenv("YUNQI_OUTBOUND_ALLOWED_PORTS", "")
	t.Setenv("YUNQI_OUTBOUND_BLOCK_PRIVATE", "false")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	defer server.Close()
	base := http.DefaultTransport.(*http.Transport).Clone()
	transport := DefaultOutboundTransport(base)
	client := &http.Client{Transport: transport, Timeout: time.Second}
	_, err := client.Get(server.URL)
	if err == nil || !IsEgressError(err) {
		t.Fatalf("metadata redirect not blocked: %v", err)
	}
	if base.DialContext == nil {
		t.Fatal("default source transport unexpectedly mutated")
	}
	if close, ok := transport.(interface{ CloseIdleConnections() }); ok {
		close.CloseIdleConnections()
	}
}

func TestOutboundPolicyActualDialBlocksDNSChange(t *testing.T) {
	t.Setenv("YUNQI_OUTBOUND_BLOCK_PRIVATE", "true")
	d := newMetadataSafeDialer()
	// ControlContext sees the actual resolved address, independent of a previous
	// URL/DNS validation and including custom resolver results.
	err := d.ControlContext(context.Background(), "tcp", "127.0.0.1:443", nil)
	if err == nil || !IsEgressError(err) {
		t.Fatalf("actual private destination allowed: %v", err)
	}
	t.Setenv("YUNQI_OUTBOUND_PRIVATE_CIDRS", "127.0.0.1/32")
	if err := d.ControlContext(context.Background(), "tcp", "127.0.0.1:443", nil); err != nil {
		t.Fatalf("private exception denied: %v", err)
	}
	if err := d.ControlContext(context.Background(), "tcp", "169.254.169.254:80", nil); err == nil {
		t.Fatal("metadata destination allowed")
	}
}

func TestOutboundPolicyUsesBoundResolverForCachedDirectRequests(t *testing.T) {
	t.Setenv("YUNQI_OUTBOUND_BLOCK_PRIVATE", "true")
	called := 0
	capture := &policyCaptureTransport{}
	transport := &metadataGuardTransport{base: capture, privateLookup: func(context.Context, string) ([]net.IPAddr, error) {
		called++
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.5")}}, nil
	}}
	req, _ := http.NewRequest(http.MethodGet, "https://bound-dns-only.invalid", nil)
	if _, err := transport.RoundTrip(req); err == nil || !IsEgressError(err) || called != 1 || capture.calls != 0 {
		t.Fatalf("bound resolver bypass: err=%v lookups=%d requests=%d", err, called, capture.calls)
	}
	t.Setenv("YUNQI_OUTBOUND_PRIVATE_CIDRS", "10.0.0.0/8")
	_, _ = transport.RoundTrip(req)
	if called != 2 || capture.calls != 1 {
		t.Fatalf("private exception did not use bound resolver: lookups=%d requests=%d", called, capture.calls)
	}
}
