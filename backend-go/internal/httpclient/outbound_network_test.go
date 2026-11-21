package httpclient

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"golang.org/x/net/dns/dnsmessage"
)

func TestOutboundProxyModes(t *testing.T) {
	var proxyCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/blocked", 302)
			return
		}
		w.WriteHeader(403)
		io.WriteString(w, "direct")
	}))
	defer upstream.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyCalls.Add(1); io.WriteString(w, "proxy") }))
	defer proxy.Close()
	for _, tc := range []struct {
		mode  string
		want  string
		count int32
	}{{"direct", "direct", 0}, {"proxy", "proxy", 1}, {"direct_first", "proxy", 1}, {"legacy", "proxy", 1}} {
		t.Run(tc.mode, func(t *testing.T) {
			proxyCalls.Store(0)
			client := GetManager().NewClient(ClientOptions{Timeout: time.Second, ProxyURL: proxy.URL, ProxyPreferDirect: true, OutboundNetwork: &config.OutboundNetworkConfig{ProxyMode: tc.mode}})
			defer client.CloseIdleConnections()
			resp, err := client.Get(upstream.URL + "/redirect")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			if string(b) != tc.want || proxyCalls.Load() != tc.count {
				t.Fatalf("body=%q proxy calls=%d", b, proxyCalls.Load())
			}
		})
	}
}
func TestOutboundMissingInterfaceNeverDialsProxyOrRotatesPool(t *testing.T) {
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer proxy.Close()
	c := &config.OutboundNetworkConfig{ProxyMode: "direct_first", InterfaceName: "yunqi-missing", DNS: config.OutboundDNSConfig{Mode: "bound", Servers: []string{"127.0.0.1:53"}}}
	client := GetManager().GetClient(ClientOptions{Timeout: time.Second, ProxyURL: proxy.URL, OutboundNetwork: c})
	_, err := client.Get("http://127.0.0.1:1")
	if !IsEgressError(err) || calls.Load() != 0 {
		t.Fatalf("err=%v proxy calls=%d", err, calls.Load())
	}
}
func TestOutboundCacheIsolationAndSnapshot(t *testing.T) {
	cm := &ClientManager{clients: map[string]*http.Client{}}
	cfg := &config.OutboundNetworkConfig{ProxyMode: "direct"}
	opts := ClientOptions{Timeout: time.Second, ProxyURL: "http://localhost:1234", OutboundNetwork: cfg}
	a := cm.GetClient(opts)
	if cm.GetClient(opts) != a {
		t.Fatal("identical policy should reuse client")
	}
	cfg.AddressFamily = "ipv4"
	b := cm.GetClient(opts)
	if a == b {
		t.Fatal("family must isolate pool")
	}
	if a.Transport.(*policyTransport).opts.OutboundNetwork.AddressFamily != "" {
		t.Fatal("cached policy mutated through caller")
	}
	opts.OutboundNetwork = &config.OutboundNetworkConfig{ProxyMode: "proxy"}
	if cm.GetClient(opts) == b {
		t.Fatal("proxy policy shared pool")
	}
	for i := 0; i < 260; i++ {
		opts.Timeout = time.Duration(i+1) * time.Second
		cm.GetClient(opts)
	}
	if len(cm.clients) > 256 {
		t.Fatal("unbounded client cache")
	}
	for _, c := range cm.clients {
		c.CloseIdleConnections()
	}
}
func TestDirectFirstLocalErrorIsTerminal(t *testing.T) {
	for _, err := range []error{egressError("bind", errors.New("denied")), errMetadataDestination, context.Canceled} {
		var calls int
		ctx, cancel := context.WithCancel(context.Background())
		if errors.Is(err, context.Canceled) {
			cancel()
		}
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://example.com", nil)
		rt := directFirstRoundTripper{direct: testRoundTripper(func(*http.Request) (*http.Response, error) { return nil, err }), proxied: testRoundTripper(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected") })}
		_, got := rt.RoundTrip(req)
		if !errors.Is(got, err) || calls != 0 {
			t.Fatalf("got=%v proxy calls=%d", got, calls)
		}
	}
}

type testRoundTripper func(*http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOutboundBoundDNSNoSystemFallback(t *testing.T) {
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	var queries atomic.Int32
	go func() {
		b := make([]byte, 4096)
		for {
			n, addr, e := server.ReadFrom(b)
			if e != nil {
				return
			}
			var q dnsmessage.Message
			if q.Unpack(b[:n]) != nil {
				continue
			}
			queries.Add(1)
			a := dnsmessage.Message{Header: dnsmessage.Header{ID: q.ID, Response: true, RecursionAvailable: true}, Questions: q.Questions}
			for _, v := range q.Questions {
				if v.Type == dnsmessage.TypeA {
					a.Answers = append(a.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: v.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 30}, Body: &dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}}})
				}
			}
			raw, _ := a.Pack()
			server.WriteTo(raw, addr)
		}
	}()
	p, err := ResolveOutboundPlan(ClientOptions{OutboundNetwork: &config.OutboundNetworkConfig{DNS: config.OutboundDNSConfig{Mode: "bound", Servers: []string{server.LocalAddr().String()}}}})
	if err != nil {
		t.Fatal(err)
	}
	ips, err := p.LookupIPAddr(context.Background(), "only-on-private-dns.invalid")
	if err != nil || len(ips) == 0 || !ips[0].IP.Equal(net.ParseIP("127.0.0.1")) || queries.Load() == 0 {
		t.Fatalf("ips=%v err=%v queries=%d", ips, err, queries.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.DialContext(ctx, "tcp", "only-on-private-dns.invalid:80")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
func TestOutboundMetadataAndDiagnosis(t *testing.T) {
	var requests atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer s.Close()
	opts := ClientOptions{OutboundNetwork: &config.OutboundNetworkConfig{ProxyMode: "direct"}}
	results := DiagnoseNetwork(context.Background(), s.URL, opts)
	if len(results) != 1 || !results[0].Success || results[0].LocalAddress == "" || results[0].BindingApplied || requests.Load() != 0 {
		t.Fatalf("results=%+v requests=%d", results, requests.Load())
	}
	c := GetManager().NewClient(opts)
	defer c.CloseIdleConnections()
	_, err := c.Get("http://169.254.169.254/latest/meta-data")
	if err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("metadata allowed: %v", err)
	}
}
func TestOutboundLoopbackBinding(t *testing.T) {
	if !interfaceBindingSupported {
		t.Skip("binding unsupported")
	}
	interfaces, e := net.Interfaces()
	if e != nil {
		t.Fatal(e)
	}
	for _, i := range interfaces {
		if i.Flags&net.FlagLoopback == 0 || i.Flags&net.FlagUp == 0 {
			continue
		}
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "bound") }))
		defer s.Close()
		opts := ClientOptions{Timeout: time.Second, OutboundNetwork: &config.OutboundNetworkConfig{ProxyMode: "direct", InterfaceName: i.Name, AddressFamily: "ipv4", DNS: config.OutboundDNSConfig{Mode: "bound", Servers: []string{"127.0.0.1:53"}}}}
		c := GetManager().NewClient(opts)
		defer c.CloseIdleConnections()
		resp, err := c.Get(s.URL)
		if IsEgressError(err) && (errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES)) {
			t.Skip("binding permission unavailable; typed local error verified")
		}
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return
	}
	t.Skip("no loopback interface")
}

func TestOutboundSOCKSUsesPolicyDialer(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "socks-bound") }))
	defer upstream.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			done <- e
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		header := make([]byte, 2)
		if _, e = io.ReadFull(c, header); e != nil {
			done <- e
			return
		}
		methods := make([]byte, int(header[1]))
		io.ReadFull(c, methods)
		c.Write([]byte{5, 0})
		head := make([]byte, 4)
		if _, e = io.ReadFull(c, head); e != nil {
			done <- e
			return
		}
		size := 4
		if head[3] == 4 {
			size = 16
		}
		if head[3] == 3 {
			b := make([]byte, 1)
			io.ReadFull(c, b)
			size = int(b[0])
		}
		target := make([]byte, size+2)
		if _, e = io.ReadFull(c, target); e != nil {
			done <- e
			return
		}
		u, e := net.Dial("tcp", strings.TrimPrefix(upstream.URL, "http://"))
		if e != nil {
			done <- e
			return
		}
		defer u.Close()
		c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
		go func() { io.Copy(u, c); u.Close() }()
		_, e = io.Copy(c, u)
		done <- e
	}()
	p, err := ResolveOutboundPlan(ClientOptions{ProxyURL: "socks5://" + listener.Addr().String(), OutboundNetwork: &config.OutboundNetworkConfig{ProxyMode: "proxy"}})
	if err != nil {
		t.Fatal(err)
	}
	var controls atomic.Int32
	original := p.dialer.ControlContext
	p.dialer.ControlContext = func(ctx context.Context, network, addr string, raw syscall.RawConn) error {
		controls.Add(1)
		return original(ctx, network, addr, raw)
	}
	rt, err := p.transport(ClientOptions{}, true)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: rt, Timeout: 2 * time.Second}
	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	client.CloseIdleConnections()
	if string(b) != "socks-bound" || controls.Load() == 0 {
		t.Fatalf("body=%q policy dialer controls=%d", b, controls.Load())
	}
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("SOCKS worker leaked")
	}
}
func TestOutboundBoundDNSRetriesTruncatedAnswersOverTCP(t *testing.T) {
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp", tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	var tcpQueries atomic.Int32
	answer := func(b []byte, truncated bool) []byte {
		var q dnsmessage.Message
		if q.Unpack(b) != nil {
			return nil
		}
		a := dnsmessage.Message{Header: dnsmessage.Header{ID: q.ID, Response: true, Truncated: truncated, RecursionAvailable: true}, Questions: q.Questions}
		if !truncated {
			for _, v := range q.Questions {
				if v.Type == dnsmessage.TypeA {
					a.Answers = append(a.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: v.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}, Body: &dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}}})
				}
			}
		}
		raw, _ := a.Pack()
		return raw
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, e := udp.ReadFrom(buf)
			if e != nil {
				return
			}
			udp.WriteTo(answer(buf[:n], true), addr)
		}
	}()
	go func() {
		for {
			c, e := tcp.Accept()
			if e != nil {
				return
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(time.Second))
				head := make([]byte, 2)
				if _, e := io.ReadFull(c, head); e != nil {
					return
				}
				b := make([]byte, int(head[0])<<8|int(head[1]))
				if _, e := io.ReadFull(c, b); e != nil {
					return
				}
				tcpQueries.Add(1)
				raw := answer(b, false)
				c.Write(append([]byte{byte(len(raw) >> 8), byte(len(raw))}, raw...))
			}()
		}
	}()
	p, err := ResolveOutboundPlan(ClientOptions{OutboundNetwork: &config.OutboundNetworkConfig{DNS: config.OutboundDNSConfig{Mode: "bound", Servers: []string{tcp.Addr().String()}}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ips, err := p.LookupIPAddr(ctx, "tcp-dns.invalid")
	if err != nil || len(ips) == 0 || tcpQueries.Load() == 0 {
		t.Fatalf("ips=%v err=%v TCP=%d", ips, err, tcpQueries.Load())
	}
}

func TestDirectFirstReplayFailurePreservesOriginalResponse(t *testing.T) {
	body := &closeAwareBody{reader: strings.NewReader("original denial")}
	req, _ := http.NewRequest("POST", "http://example.com", strings.NewReader("payload"))
	req.GetBody = func() (io.ReadCloser, error) { return nil, errors.New("cannot replay") }
	calls := 0
	rt := directFirstRoundTripper{direct: testRoundTripper(func(*http.Request) (*http.Response, error) { return &http.Response{StatusCode: 403, Body: body}, nil }), proxied: testRoundTripper(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected") })}
	resp, err := rt.RoundTrip(req)
	if err != nil || resp == nil {
		t.Fatalf("response lost: %v", err)
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(resp.Body)
	if e != nil || string(b) != "original denial" || calls != 0 {
		t.Fatalf("response=%q err=%v proxy=%d", b, e, calls)
	}
}

type closeAwareBody struct {
	reader *strings.Reader
	closed bool
}

func (b *closeAwareBody) Read(p []byte) (int, error) {
	if b.closed {
		return 0, io.ErrClosedPipe
	}
	return b.reader.Read(p)
}
func (b *closeAwareBody) Close() error { b.closed = true; return nil }

func TestOutboundPolicyChangeAndEvictionDoNotCancelActiveSSE(t *testing.T) {
	finish := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(finish) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: ready\n\n")
		w.(http.Flusher).Flush()
		<-finish
		io.WriteString(w, "data: complete\n\n")
	}))
	defer server.Close()
	defer release()
	cm := &ClientManager{clients: map[string]*http.Client{}}
	opts := ClientOptions{Stream: true, Timeout: time.Millisecond, OutboundNetwork: &config.OutboundNetworkConfig{ProxyMode: "direct"}}
	old := cm.GetClient(opts)
	defer old.CloseIdleConnections()
	if old.Timeout != 0 {
		t.Fatal("SSE inherited non-stream timeout")
	}
	resp, err := old.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != "data: ready\n" {
		t.Fatalf("line=%q err=%v", line, err)
	}
	opts.OutboundNetwork.AddressFamily = "ipv4"
	if cm.GetClient(opts) == old {
		t.Fatal("policy update reused old client")
	}
	for i := 0; i < 260; i++ {
		cm.GetClient(ClientOptions{Timeout: time.Duration(i+1) * time.Second, OutboundNetwork: &config.OutboundNetworkConfig{ProxyMode: "direct"}})
	}
	release()
	remaining, err := io.ReadAll(reader)
	if err != nil || !strings.Contains(string(remaining), "data: complete") {
		t.Fatalf("active SSE interrupted: body=%q err=%v", remaining, err)
	}
	for _, client := range cm.clients {
		client.CloseIdleConnections()
	}
}

func TestNetworkDiagnosisPreservesLocalFailureStage(t *testing.T) {
	opts := ClientOptions{OutboundNetwork: &config.OutboundNetworkConfig{InterfaceName: "yunqi-missing", DNS: config.OutboundDNSConfig{Mode: "bound", Servers: []string{"127.0.0.1:53"}}}}
	result := DiagnoseNetwork(context.Background(), "https://example.com", opts)
	if len(result) != 1 || result[0].Stage != "interface" || !strings.Contains(result[0].Error, "network_egress_unavailable") {
		t.Fatalf("diagnostic failure classification lost: %+v", result)
	}
}
