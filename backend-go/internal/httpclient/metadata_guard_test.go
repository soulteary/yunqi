package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetadataGuardAllClientVariants(t *testing.T) {
	for _, variant := range []string{"standard", "uncached", "stream", "direct-first", "uncached-direct-first", "socks"} {
		t.Run(variant, func(t *testing.T) {
			cm := newTestManager()
			var client *http.Client
			switch variant {
			case "standard":
				client = cm.GetStandardClient(time.Second, false)
			case "uncached":
				client = cm.NewStandardClient(time.Second, false)
			case "stream":
				client = cm.GetStreamClient(false)
			case "socks":
				client = cm.GetStandardClient(time.Second, false, "socks5h://127.0.0.1:9999")
			case "direct-first":
				client = cm.GetClient(ClientOptions{Timeout: time.Second, ProxyURL: "http://127.0.0.1:9999", ProxyPreferDirect: true})
			case "uncached-direct-first":
				client = cm.NewClient(ClientOptions{Timeout: time.Second, ProxyURL: "http://127.0.0.1:9999", ProxyPreferDirect: true})
			}
			calls := 0
			stub := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://[fd00:ec2::254]/"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
			})
			// Replace only the network transport. A regression cannot contact metadata.
			switch rt := client.Transport.(type) {
			case *metadataGuardTransport:
				rt.base = stub
			case *directFirstRoundTripper:
				rt.direct.(*metadataGuardTransport).base = stub
				rt.proxied.(*metadataGuardTransport).base = stub
			default:
				t.Fatalf("unguarded transport %T", rt)
			}
			_, err := client.Get("http://[fd00:ec2::254]/")
			if !errors.Is(err, errMetadataDestination) || calls != 0 {
				t.Fatalf("initial metadata target: err=%v calls=%d", err, calls)
			}
			_, err = client.Get("http://127.0.0.1:12345/start")
			if !errors.Is(err, errMetadataDestination) || calls != 1 {
				t.Fatalf("metadata redirect: err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestMetadataDialChecksResolvedAddress(t *testing.T) {
	dialer := newMetadataSafeDialer()
	// Simulate the address selected by DNS changing after URL validation.
	// Invoke ControlContext directly: no metadata network connection is attempted.
	for _, addr := range []string{"169.254.169.254:80", "[fd00:ec2::254]:80", "[::ffff:169.254.169.254]:80"} {
		if err := dialer.ControlContext(context.Background(), "tcp", addr, nil); !errors.Is(err, errMetadataDestination) {
			t.Fatalf("resolved address %s: %v", addr, err)
		}
	}
	for _, addr := range []string{"127.0.0.1:11434", "192.168.1.2:80", "[::1]:80", "[fd00::1]:80"} {
		if err := dialer.ControlContext(context.Background(), "tcp", addr, nil); err != nil {
			t.Fatalf("allowed address %s: %v", addr, err)
		}
	}
	if dialer.Timeout != 30*time.Second || dialer.FallbackDelay < 0 {
		t.Fatal("standard dial timeout / Happy Eyeballs disabled")
	}
}

func TestMetadataGuardAllowsLocalHTTPAndRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/finish", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "local upstream")
	}))
	defer server.Close()
	client := newTestManager().GetStandardClient(time.Second, false)
	defer client.CloseIdleConnections()
	resp, err := client.Get(server.URL + "/start")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "local upstream" {
		t.Fatalf("body %q", body)
	}
}

func TestProxyMetadataGuardPreservesRemoteDNSButRejectsKnownTargets(t *testing.T) {
	calls := 0
	rt := &metadataGuardTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	}),
		lookup: func(context.Context, string) ([]net.IPAddr, error) {
			return nil, &net.DNSError{Err: "proxy-only DNS", IsNotFound: true}
		}}
	req, _ := http.NewRequest(http.MethodGet, "http://service.corporate.invalid/", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"http://metadata.google.internal./", "http://100.100.100.200/", "http://169.254.169.254/"} {
		req, _ = http.NewRequest(http.MethodGet, target, nil)
		if _, err := rt.RoundTrip(req); !errors.Is(err, errMetadataDestination) {
			t.Fatalf("%s: %v", target, err)
		}
	}
	if calls != 1 {
		t.Fatalf("proxy network calls=%d", calls)
	}
	rt.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}, nil
	}
	req, _ = http.NewRequest(http.MethodGet, "http://alias.internal/", nil)
	if _, err := rt.RoundTrip(req); !errors.Is(err, errMetadataDestination) {
		t.Fatalf("DNS alias: %v", err)
	}
	if calls != 1 {
		t.Fatalf("DNS alias reached proxy")
	}
}

func TestMetadataRejectionNeverFallsBackToProxy(t *testing.T) {
	proxyCalls := 0
	rt := &directFirstRoundTripper{direct: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("dial blocked: %w", errMetadataDestination) }),
		proxied: roundTripFunc(func(*http.Request) (*http.Response, error) { proxyCalls++; return nil, errors.New("unexpected proxy") })}
	req, _ := http.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	if _, err := rt.RoundTrip(req); !errors.Is(err, errMetadataDestination) || proxyCalls != 0 {
		t.Fatalf("err=%v proxy calls=%d", err, proxyCalls)
	}
}
