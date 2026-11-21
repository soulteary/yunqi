package telemetry

import (
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"sync"
)

// NetworkTiming composes with existing ClientTrace hooks. Reused connections do
// not invent DNS/connect/TLS values. finish closes callbacks skipped on errors.
func NetworkTiming(req *http.Request) (*http.Request, func()) {
	if req == nil || Timing(req.Context()) == nil {
		return req, func() {}
	}
	var mu sync.Mutex
	ends := map[string][]func(){}
	begin := func(key, phase string) {
		mu.Lock()
		defer mu.Unlock()
		ends[key] = append(ends[key], Phase(req.Context(), phase))
	}
	end := func(key string) {
		mu.Lock()
		list := ends[key]
		delete(ends, key)
		mu.Unlock()
		for _, finish := range list {
			finish()
		}
	}
	trace := &httptrace.ClientTrace{
		GetConn: func(string) { begin("pool", "queue") }, GotConn: func(httptrace.GotConnInfo) { end("pool") },
		DNSStart: func(httptrace.DNSStartInfo) { begin("dns", "dns") }, DNSDone: func(httptrace.DNSDoneInfo) { end("dns") },
		ConnectStart: func(network, addr string) { begin(network+addr, "connect") }, ConnectDone: func(network, addr string, err error) { end(network + addr) },
		TLSHandshakeStart: func() { begin("tls", "tls") }, TLSHandshakeDone: func(tls.ConnectionState, error) { end("tls") },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				begin("headers", "upstream_wait")
			}
		}, GotFirstResponseByte: func() { end("headers") },
	}
	finish := func() {
		mu.Lock()
		pending := ends
		ends = map[string][]func(){}
		mu.Unlock()
		for _, list := range pending {
			for _, end := range list {
				end()
			}
		}
	}
	return req.WithContext(httptrace.WithClientTrace(req.Context(), trace)), finish
}
