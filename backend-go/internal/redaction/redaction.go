// Package redaction protects diagnostic/error outputs independently of optional
// content guardrails. Successful model responses and credential export APIs are
// not routed through this boundary.
package redaction

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const Mask = "[REDACTED]"
const CapacityMask = "[REDACTED: credential registry capacity exceeded]"
const MaxCredentials = 4096
const MaxCredentialBytes = 512 * 1024
const MaxSingleCredentialBytes = 16 * 1024
const MaxMatcherEntries = 16 * 1024
const MaxMatcherBytes = 2 * 1024 * 1024

type snapshot struct {
	values       []string
	replacer     *strings.Replacer
	short        []string
	known        map[string]bool
	rawBytes     int
	matcherBytes int
	matcherCount int
	failClosed   bool
	reason       string
}

var current atomic.Pointer[snapshot]
var enabled atomic.Bool
var updateMu sync.Mutex
var userinfo = regexp.MustCompile(`(?i)((?:https?|socks5h?)://)[^\s/"]*@`)
var jwt = regexp.MustCompile(`[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)
var bearer = regexp.MustCompile(`(?i)(Bearer[ \t]+)[A-Za-z0-9._~+/-]+=*`)
var commonKey = regexp.MustCompile(`(?:sk-[A-Za-z0-9_-]{12,}|gh[pousr]_[A-Za-z0-9]{16,})`)

// Configure installs current gateway credentials. ObserveJSON adds full current
// upstream credentials, including unrecognised vendor formats. Retaining observed
// values also protects an in-flight request after its credential is rotated.
func Configure(values []string) {
	updateMu.Lock()
	defer updateMu.Unlock()
	store(values)
	enabled.Store(true)
}

// Enable protects staged startup without replacing credentials already observed
// from the initial config read or clearing a fail-closed capacity state. The
// returned closure supports isolated callers/tests; services keep it enabled.
func Enable(values []string) func() {
	updateMu.Lock()
	old, wasEnabled := current.Load(), enabled.Load()
	enabled.Store(true)
	updateMu.Unlock()
	Add(values)
	return func() { updateMu.Lock(); current.Store(old); enabled.Store(wasEnabled); updateMu.Unlock() }
}

func Add(values []string) {
	updateMu.Lock()
	defer updateMu.Unlock()
	old := current.Load()
	if old != nil && old.failClosed {
		return
	}
	seen := make(map[string]bool)
	var merged []string
	total := 0
	if old != nil {
		seen = old.known
		total = old.rawBytes
	}
	// Inspect new values before cloning/building aliases. No unbounded caller slice
	// is appended to retained state, including a hostile validation credential.
	fresh := make([]string, 0)
	newSeen := make(map[string]bool)
	count := len(seen)
	for _, value := range values {
		if value == "" || seen[value] || newSeen[value] {
			continue
		}
		if len(value) > MaxSingleCredentialBytes {
			closeRegistry("single_credential_bytes")
			return
		}
		if count+len(fresh)+1 > MaxCredentials {
			closeRegistry("credential_count")
			return
		}
		if total+len(value) > MaxCredentialBytes {
			closeRegistry("credential_bytes")
			return
		}
		total += len(value)
		newSeen[value] = true
		fresh = append(fresh, value)
	}
	if len(fresh) == 0 {
		return
	}
	if old != nil {
		merged = append(merged, old.values...)
	}
	merged = append(merged, fresh...)
	store(merged)
}
func closeRegistry(reason string) { current.Store(&snapshot{failClosed: true, reason: reason}) }

type RegistryStatistics struct {
	Credentials     int    `json:"credentials"`
	CredentialBytes int    `json:"credentialBytes"`
	MatcherEntries  int    `json:"matcherEntries"`
	MatcherBytes    int    `json:"matcherBytes"`
	FailClosed      bool   `json:"failClosed"`
	Reason          string `json:"reason,omitempty"`
}

func RegistryStats() RegistryStatistics {
	if state := current.Load(); state != nil {
		return RegistryStatistics{len(state.values), state.rawBytes, state.matcherCount, state.matcherBytes, state.failClosed, state.reason}
	}
	return RegistryStatistics{}
}
func capacityText(value string) string {
	if value == "" {
		return ""
	}
	if strings.HasSuffix(value, "\n") {
		return CapacityMask + "\n"
	}
	return CapacityMask
}

func store(values []string) {
	seen := map[string]bool{}
	rawSeen := map[string]bool{}
	rawValues := make([]string, 0, min(len(values), MaxCredentials))
	unique := make([]string, 0, min(len(values), MaxCredentials))
	rawBytes, matcherBytes := 0, 0
	for _, value := range values {
		if value == "" || rawSeen[value] {
			continue
		}
		if len(value) > MaxSingleCredentialBytes {
			closeRegistry("single_credential_bytes")
			return
		}
		if len(rawValues)+1 > MaxCredentials {
			closeRegistry("credential_count")
			return
		}
		if rawBytes+len(value) > MaxCredentialBytes {
			closeRegistry("credential_bytes")
			return
		}
		rawBytes += len(value)
		rawSeen[value] = true
		rawValues = append(rawValues, value)
		quoted, _ := json.Marshal(value)
		variants := []string{value, url.QueryEscape(value), url.PathEscape(value), string(quoted[1 : len(quoted)-1])}
		for _, variant := range variants {
			if !seen[variant] {
				if len(unique)+1 > MaxMatcherEntries {
					closeRegistry("matcher_entries")
					return
				}
				if matcherBytes+len(variant) > MaxMatcherBytes {
					closeRegistry("matcher_bytes")
					return
				}
				matcherBytes += len(variant)
				seen[variant] = true
				unique = append(unique, variant)
			}
		}
	}
	sort.Slice(unique, func(i, j int) bool { return len(unique[i]) > len(unique[j]) })
	pairs := make([]string, 0, len(unique)*2)
	short := make([]string, 0)
	for _, value := range unique {
		if len(value) < 4 {
			short = append(short, value)
		} else {
			pairs = append(pairs, value, Mask)
		}
	}
	current.Store(&snapshot{values: rawValues, replacer: strings.NewReplacer(pairs...), short: short, known: rawSeen, rawBytes: rawBytes, matcherBytes: matcherBytes, matcherCount: len(unique)})
}
func String(value string) string {
	if state := current.Load(); state != nil {
		if state.failClosed {
			return capacityText(value)
		}
		value = state.replacer.Replace(value)
		for _, key := range state.short {
			value = replaceShortToken(value, key)
		}
	}
	value = userinfo.ReplaceAllString(value, "${1}"+Mask+"@")
	value = jwt.ReplaceAllString(value, Mask)
	value = bearer.ReplaceAllString(value, "${1}"+Mask)
	return commonKey.ReplaceAllString(value, Mask)
}
func tokenByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}
func replaceShortToken(value, key string) string {
	var out strings.Builder
	offset := 0
	for offset < len(value) {
		at := strings.Index(value[offset:], key)
		if at < 0 {
			out.WriteString(value[offset:])
			break
		}
		at += offset
		end := at + len(key)
		out.WriteString(value[offset:at])
		if (at == 0 || !tokenByte(value[at-1])) && (end == len(value) || !tokenByte(value[end])) {
			out.WriteString(Mask)
		} else {
			out.WriteString(key)
		}
		offset = end
	}
	return out.String()
}
func sensitiveField(key string) bool {
	key = strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	switch key {
	case "key", "xapikey", "apikey", "apikeys", "historicalapikeys", "authorization", "proxyauthorization", "cookie", "setcookie", "password", "secret", "secretaccesskey", "accesskey", "accesskeyid", "clientsecret", "authcookie":
		return true
	}
	return strings.HasSuffix(key, "token")
}
func ObserveJSON(data []byte) {
	if !enabled.Load() || RegistryStats().FailClosed {
		return
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		return
	}
	var values []string
	var collect func(any, bool)
	collect = func(value any, secret bool) {
		switch v := value.(type) {
		case string:
			if secret {
				values = append(values, v)
				if parts := strings.Fields(v); len(parts) == 2 && (strings.EqualFold(parts[0], "Bearer") || strings.EqualFold(parts[0], "Basic")) {
					values = append(values, parts[1])
				}
			}
		case []any:
			for _, item := range v {
				collect(item, secret)
			}
		case map[string]any:
			for key, item := range v {
				if strings.EqualFold(key, "customHeaders") {
					if headers, ok := item.(map[string]any); ok {
						for name, value := range headers {
							if SensitiveHeader(name) {
								collect(value, true)
							}
						}
					}
					continue
				}
				collect(item, secret || sensitiveField(key))
			}
		}
	}
	collect(value, false)
	Add(values)
}
func decode(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	err := decoder.Decode(&value)
	return value, err
}
func sanitize(value any) (any, bool) {
	changed := false
	switch v := value.(type) {
	case string:
		safe := String(v)
		return safe, safe != v
	case json.Number:
		safe := String(v.String())
		if safe != v.String() {
			return safe, true
		}
	case []any:
		for i := range v {
			safe, modified := sanitize(v[i])
			v[i] = safe
			changed = changed || modified
		}
	case map[string]any:
		for key, item := range v {
			if RegistryStats().FailClosed && key == "type" && publicProtocolType(item) {
				continue
			}
			if sensitiveField(key) && item != nil {
				if text, ok := item.(string); !ok || text != Mask {
					v[key] = Mask
					changed = true
				}
			} else {
				safe, modified := sanitize(item)
				v[key] = safe
				changed = changed || modified
			}
		}
	}
	return value, changed
}
func JSON(data []byte) []byte {
	value, err := decode(data)
	if err != nil {
		return []byte(String(string(data)))
	}
	value, changed := sanitize(value)
	if !changed {
		return data
	}
	result, err := json.Marshal(value)
	if err != nil {
		return []byte(Mask)
	}
	return result
}

func isError(value any) bool {
	v, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if err, found := v["error"]; found && err != nil {
		return true
	}
	if typ, ok := v["type"].(string); ok && (typ == "error" || strings.HasSuffix(typ, ".failed") || strings.HasSuffix(typ, ".error")) {
		return true
	}
	if response, ok := v["response"].(map[string]any); ok {
		if err, found := response["error"]; found && err != nil {
			return true
		}
	}
	return false
}
func ErrorJSON(data []byte) []byte {
	value, err := decode(data)
	if err == nil && isError(value) {
		return JSON(data)
	}
	return data
}

// SSEErrorEvent receives a complete parsed event (or complete SSE data line),
// so credentials split over network chunks are reassembled before redaction.
func SSEErrorEvent(event string) string {
	lines := strings.Split(event, "\n")
	r := &SSELineRedactor{}
	for i, line := range lines {
		lines[i] = r.Line(line)
	}
	return strings.Join(lines, "\n")
}

type writer struct{ io.Writer }

func Writer(destination io.Writer) io.Writer { return writer{destination} }
func (w writer) Write(data []byte) (int, error) {
	_, err := w.Writer.Write([]byte(String(string(data))))
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

// SSELineRedactor tracks an explicit error event across separately delivered
// complete lines. Callers retain their existing network-fragment line buffer.
type SSELineRedactor struct{ errorEvent bool }

func (r *SSELineRedactor) Line(line string) string {
	if strings.TrimSpace(line) == "" {
		r.errorEvent = false
		return line
	}
	if strings.HasPrefix(line, "event:") {
		name := strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		r.errorEvent = name == "error" || strings.HasSuffix(name, ".error") || strings.HasSuffix(name, ".failed")
		return line
	}
	if !strings.HasPrefix(line, "data:") {
		return line
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "" || data == "[DONE]" {
		return line
	}
	if r.errorEvent {
		return "data: " + string(JSON([]byte(data)))
	}
	if safe := ErrorJSON([]byte(data)); !bytes.Equal(safe, []byte(data)) {
		return "data: " + string(safe)
	}
	return line
}

func (w writer) Close() error {
	if closer, ok := w.Writer.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// ObserveRequest protects the exact authentication used by this attempt, including
// unsaved validation credentials and query-authenticated Gemini requests. Ordinary
// forwarded client headers/cookies are not retained as credential candidates.
func ObserveRequest(req *http.Request) {
	if !enabled.Load() || req == nil || RegistryStats().FailClosed {
		return
	}
	var values []string
	add := func(value string) {
		if value == "" {
			return
		}
		if parts := strings.Fields(value); len(parts) == 2 && (strings.EqualFold(parts[0], "Bearer") || strings.EqualFold(parts[0], "Basic")) {
			values = append(values, parts[1])
			return
		}
		values = append(values, value)
	}
	if username, password, ok := req.BasicAuth(); ok {
		add(username)
		add(password)
	}
	for name, items := range req.Header {
		if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Proxy-Authorization") || strings.EqualFold(name, "X-Api-Key") || strings.EqualFold(name, "Api-Key") {
			for _, value := range items {
				add(value)
			}
		}
	}
	if req.URL != nil {
		for name, items := range req.URL.Query() {
			if strings.EqualFold(name, "key") || strings.EqualFold(name, "api_key") || strings.EqualFold(name, "apiKey") {
				for _, value := range items {
					add(value)
				}
			}
		}
		if req.URL.User != nil {
			add(req.URL.User.Username())
			if password, ok := req.URL.User.Password(); ok {
				add(password)
			}
		}
	}
	Add(values)
}

func publicProtocolType(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	switch text {
	case "error", "stream_error", "response.error", "response.failed", "response.incomplete", "invalid_request_error", "upstream_error", "api_error", "authentication_error", "permission_error", "rate_limit_error", "overloaded_error":
		return true
	}
	return false
}

// BodyJSON is stricter than client-error redaction: logging never needs binary
// media or common structured personal identifiers, even after explicit opt-in.
func BodyJSON(data []byte) []byte {
	value, err := decode(data)
	if err != nil {
		return []byte("[OMITTED: non-JSON body, " + strconv.Itoa(len(data)) + " bytes]")
	}
	var scrub func(any) any
	scrub = func(value any) any {
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				name := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
				switch name {
				case "data", "b64json", "base64", "inlinedata", "filedata", "imageurl", "inputaudio", "audiodata", "attachments":
					v[key] = "[OMITTED:media]"
				case "email", "phone", "phonenumber", "address", "ssn", "creditcard", "creditcardnumber":
					v[key] = Mask
				default:
					v[key] = scrub(item)
				}
			}
		case []any:
			for i := range v {
				v[i] = scrub(v[i])
			}
		}
		return value
	}
	value = scrub(value)
	value, _ = sanitize(value)
	result, err := json.Marshal(value)
	if err != nil {
		return []byte(Mask)
	}
	return result
}

func SensitiveHeader(name string) bool {
	name = strings.ToLower(name)
	return name == "cookie" || name == "set-cookie" || strings.Contains(name, "auth") || strings.Contains(name, "credential") || strings.HasSuffix(name, "-key") || strings.HasSuffix(name, "-token") || strings.HasSuffix(name, "-secret")
}
