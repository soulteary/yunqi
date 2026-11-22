package config

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

const ManagementAuditMaxEntries = 1000
const ManagementAuditMaxBytes = 2 << 20
const managementAuditMaxDiffs = 64

type ManagementAuditContext struct {
	configReload bool
	OperationID  string `json:"operationId"`
	ActorID      string `json:"actorId"`
	Action       string `json:"action"`
	Resource     string `json:"resource"`
}
type managementAuditContextKey struct{}

func WithManagementAudit(ctx context.Context, audit ManagementAuditContext) context.Context {
	return context.WithValue(ctx, managementAuditContextKey{}, audit)
}
func ManagementAuditFromContext(ctx context.Context) ManagementAuditContext {
	v, _ := ctx.Value(managementAuditContextKey{}).(ManagementAuditContext)
	return v
}
func NewManagementAuditContext(actor, action, resource string) (ManagementAuditContext, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ManagementAuditContext{}, err
	}
	return ManagementAuditContext{OperationID: "op_" + hex.EncodeToString(random[:]), ActorID: actor, Action: action, Resource: resource}, nil
}
func (a ManagementAuditContext) normalized() ManagementAuditContext {
	if a.OperationID == "" {
		var random [16]byte
		if _, err := rand.Read(random[:]); err == nil {
			a.OperationID = "op_" + hex.EncodeToString(random[:])
		} else {
			a.OperationID = fmt.Sprintf("system_%d", time.Now().UnixNano())
		}
	}
	if a.ActorID == "" {
		a.ActorID = "system"
	}
	if a.Action == "" {
		a.Action = "config.persist"
	}
	if a.Resource == "" {
		a.Resource = "config"
	}
	for _, p := range []*string{&a.ActorID, &a.Action, &a.Resource, &a.OperationID} {
		if len(*p) > 256 || strings.ContainsAny(*p, "\r\n\x00") {
			*p = "hash:" + EntryKeyHash(*p)
		}
	}
	return a
}

type ManagementAuditDiff struct {
	Path   string `json:"path"`
	Before string `json:"before"`
	After  string `json:"after"`
}
type ManagementAuditEvent struct {
	ManagementAuditContext
	Sequence     uint64                `json:"sequence"`
	At           time.Time             `json:"at"`
	Outcome      string                `json:"outcome"`
	Status       int                   `json:"status,omitempty"`
	BeforeDigest string                `json:"beforeDigest,omitempty"`
	AfterDigest  string                `json:"afterDigest,omitempty"`
	Differences  []ManagementAuditDiff `json:"differences,omitempty"`
	Truncated    bool                  `json:"truncated,omitempty"`
}
type ManagementAuditState struct {
	NextSequence uint64                 `json:"nextSequence"`
	Events       []ManagementAuditEvent `json:"events"`
}

func cloneManagementAudit(state *ManagementAuditState) *ManagementAuditState {
	if state == nil {
		return nil
	}
	copy := *state
	copy.Events = append([]ManagementAuditEvent(nil), state.Events...)
	for i := range copy.Events {
		copy.Events[i].Differences = append([]ManagementAuditDiff(nil), copy.Events[i].Differences...)
	}
	return &copy
}
func appendManagementAudit(state *ManagementAuditState, event ManagementAuditEvent) *ManagementAuditState {
	next := cloneManagementAudit(state)
	if next == nil {
		next = &ManagementAuditState{}
	}
	next.NextSequence++
	event.Sequence = next.NextSequence
	event.At = time.Now().UTC()
	event.ManagementAuditContext = event.ManagementAuditContext.normalized()
	next.Events = append(next.Events, event)
	for len(next.Events) > ManagementAuditMaxEntries {
		next.Events = next.Events[1:]
	}
	for {
		encoded, _ := json.Marshal(next)
		if len(encoded) <= ManagementAuditMaxBytes || len(next.Events) == 1 {
			break
		}
		next.Events = next.Events[1:]
	}
	return next
}

// RecordManagementRequest persists request intent/outcome independently of
// business configuration publication. The business commit itself appends its
// success event in the SAME config rename, so a crash cannot publish an
// unaudited successful configuration update or mistake an HTTP 200 for one.
func (cm *ConfigManager) RecordManagementRequest(audit ManagementAuditContext, outcome string, status int) error {
	switch outcome {
	case "intent", "rejected", "failed", "completed", "accepted":
	default:
		return errors.New("invalid management audit outcome")
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	data, err := os.ReadFile(cm.configFile)
	if err != nil {
		return err
	}
	if err = cm.checkAuditPublishedDocument(data); err != nil {
		return err
	}
	var document map[string]json.RawMessage
	if json.Unmarshal(data, &document) != nil || document == nil {
		return errors.New("invalid audit configuration file")
	}
	next := appendManagementAudit(cm.authoritativeManagementAuditLocked(), ManagementAuditEvent{ManagementAuditContext: audit, Outcome: outcome, Status: status})
	encoded, err := json.Marshal(next)
	if err != nil {
		return err
	}
	document["managementAudit"] = encoded
	data, err = json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err = writeConfigAtomically(cm.configFile, data); err != nil {
		return err
	}
	cm.config.ManagementAudit = next
	if cm.auditCommitted != nil {
		cm.auditCommitted.ManagementAudit = cloneManagementAudit(next)
	}
	cm.recordSelfWrite(data)
	cm.recordAuditPublishedDocument(data)
	return nil
}
func (cm *ConfigManager) ManagementAuditSnapshot() ManagementAuditState {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	copy := cloneManagementAudit(cm.authoritativeManagementAuditLocked())
	if copy == nil {
		return ManagementAuditState{Events: []ManagementAuditEvent{}}
	}
	return *copy
}
func auditJSONDocument(data []byte) (map[string]interface{}, error) {
	var doc map[string]interface{}
	if len(data) == 0 {
		return map[string]interface{}{}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return nil, err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("invalid audit source trailing data")
	}
	if doc == nil {
		return nil, errors.New("invalid audit source")
	}
	delete(doc, "managementAudit")
	return doc, nil
}
func auditDigest(value interface{}) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func auditSummary(value interface{}, present bool) string {
	if !present {
		return "absent"
	}
	data, _ := json.Marshal(value)
	return fmt.Sprintf("sha256:%s bytes:%d", auditDigest(value), len(data))
}
func auditFieldType(t reflect.Type, key string) (reflect.Type, bool) {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil, false
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if tag == "-" {
			continue
		}
		if tag == "" {
			tag = f.Name
		}
		if tag == key {
			return f.Type, true
		}
	}
	return nil, false
}
func managementAuditDiff(before, after interface{}) ([]ManagementAuditDiff, bool) {
	differences := []ManagementAuditDiff{}
	truncated := false
	visited := 0
	var walk func(string, reflect.Type, interface{}, bool, interface{}, bool)
	walk = func(path string, t reflect.Type, left interface{}, leftPresent bool, right interface{}, rightPresent bool) {
		if len(differences) >= managementAuditMaxDiffs || visited >= 100000 {
			truncated = true
			return
		}
		visited++
		if leftPresent == rightPresent && auditDigest(left) == auditDigest(right) {
			return
		}
		if len(path) > 256 {
			path = path[:200] + "[hash:" + EntryKeyHash(path)[:32] + "]"
		}
		for t != nil && t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		lm, lok := left.(map[string]interface{})
		rm, rok := right.(map[string]interface{})
		if (lok || !leftPresent) && (rok || !rightPresent) && t != nil && (t.Kind() == reflect.Struct || t.Kind() == reflect.Map) {
			keys := map[string]bool{}
			for k := range lm {
				keys[k] = true
			}
			for k := range rm {
				keys[k] = true
			}
			ordered := []string{}
			for k := range keys {
				ordered = append(ordered, k)
			}
			sort.Strings(ordered)
			for _, key := range ordered {
				lv, lp := lm[key]
				rv, rp := rm[key]
				segment := "[hash:" + EntryKeyHash(key)[:24] + "]"
				var child reflect.Type
				if t.Kind() == reflect.Struct {
					if field, known := auditFieldType(t, key); known {
						segment = key
						child = field
					}
				} else {
					child = t.Elem()
				}
				next := segment
				if path != "" {
					next = path + "." + segment
				}
				walk(next, child, lv, lp, rv, rp)
			}
			return
		}
		la, lok := left.([]interface{})
		ra, rok := right.([]interface{})
		if (lok || !leftPresent) && (rok || !rightPresent) && t != nil && (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
			for i := 0; i < max(len(la), len(ra)); i++ {
				var lv, rv interface{}
				lp, rp := i < len(la), i < len(ra)
				if lp {
					lv = la[i]
				}
				if rp {
					rv = ra[i]
				}
				walk(path+"["+strconv.Itoa(i)+"]", t.Elem(), lv, lp, rv, rp)
			}
			return
		}
		differences = append(differences, ManagementAuditDiff{Path: path, Before: auditSummary(left, leftPresent), After: auditSummary(right, rightPresent)})
	}
	walk("", reflect.TypeOf(Config{}), before, true, after, true)
	return differences, truncated
}
func (cm *ConfigManager) attachManagementAudit(audit ManagementAuditContext, persisted *Config, runtime *Config) error {
	old := cm.auditPublishedDocument
	var err error
	if len(old) == 0 {
		old, err = os.ReadFile(cm.configFile)
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	before, err := auditJSONDocument(old)
	if err != nil {
		return err
	}
	persisted.ManagementAudit = nil
	encoded, err := json.Marshal(persisted)
	if err != nil {
		return err
	}
	after, err := auditJSONDocument(encoded)
	if err != nil {
		return err
	}
	persisted.ManagementAudit = cloneManagementAudit(cm.authoritativeManagementAuditLocked())
	if auditDigest(before) != auditDigest(after) {
		diff, truncated := managementAuditDiff(before, after)
		persisted.ManagementAudit = appendManagementAudit(persisted.ManagementAudit, ManagementAuditEvent{ManagementAuditContext: audit, Outcome: "committed", BeforeDigest: auditDigest(before), AfterDigest: auditDigest(after), Differences: diff, Truncated: truncated})
	}
	runtime.ManagementAudit = cloneManagementAudit(persisted.ManagementAudit)
	return nil
}

func (cm *ConfigManager) authoritativeManagementAuditLocked() *ManagementAuditState {
	if cm.auditCommitted != nil {
		return cm.auditCommitted.ManagementAudit
	}
	return cm.config.ManagementAudit
}

var ErrManagementAuditReloadPending = errors.New("configuration reload pending; retry after reload")

func (cm *ConfigManager) checkAuditPublishedDocument(data []byte) error {
	if cm.auditDiskBusinessDigest == "" {
		return nil
	}
	doc, err := auditJSONDocument(data)
	if err != nil {
		return err
	}
	if auditDigest(doc) != cm.auditDiskBusinessDigest {
		return ErrManagementAuditReloadPending
	}
	return nil
}
func (cm *ConfigManager) recordAuditPublishedDocument(data []byte) {
	doc, err := auditJSONDocument(data)
	if err != nil {
		return
	}
	cm.auditDiskBusinessDigest = auditDigest(doc)
	cm.auditPublishedDocument = append([]byte(nil), data...)
}
