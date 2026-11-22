package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

const MaxImportedModels = 2048

var ErrModelImportConflict = errors.New("model import review is stale; reload and preview again")
var ErrModelImportNotFound = errors.New("model import channel not found")

type ModelImportRule struct {
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
}
type ModelImportPolicy struct {
	Enabled     bool              `json:"enabled"`
	Include     []string          `json:"include,omitempty"`
	Exclude     []string          `json:"exclude,omitempty"`
	StripPrefix string            `json:"stripPrefix,omitempty"`
	StripSuffix string            `json:"stripSuffix,omitempty"`
	Case        string            `json:"case,omitempty"`
	Rules       []ModelImportRule `json:"rules,omitempty"`
}

// Endpoint metadata describes evidence, never a routing whitelist inferred from
// a provider name. A declared/configured endpoint is not a successful probe.
type ModelEndpointMetadata struct {
	Type     string `json:"type"`
	Source   string `json:"source"`
	Verified bool   `json:"verified"`
}
type ModelImportCandidate struct {
	ID        string                  `json:"id"`
	Endpoints []ModelEndpointMetadata `json:"endpoints,omitempty"`
}
type ImportedModel struct {
	ID         string                  `json:"id"`
	UpstreamID string                  `json:"upstreamId"`
	Endpoints  []ModelEndpointMetadata `json:"endpoints,omitempty"`
}
type ModelImportDiff struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Changed []string `json:"changed"`
}
type ModelImportReview struct {
	ID                      string          `json:"id"`
	BaseRevision            string          `json:"baseRevision"`
	ObservedAt              time.Time       `json:"observedAt"`
	Source                  string          `json:"source"`
	Models                  []ImportedModel `json:"models"`
	Diff                    ModelImportDiff `json:"diff"`
	PreviousSupportedModels []string        `json:"previousSupportedModels,omitempty"`
}
type ModelImportAudit struct {
	OperationID string    `json:"operationId,omitempty"`
	At          time.Time `json:"at"`
	ActorID     string    `json:"actorId"`
	Action      string    `json:"action"`
	ReviewID    string    `json:"reviewId,omitempty"`
	ModelCount  int       `json:"modelCount"`
}
type ModelImportState struct {
	Policy   ModelImportPolicy  `json:"policy"`
	Approved []ImportedModel    `json:"approved,omitempty"`
	Pending  *ModelImportReview `json:"pending,omitempty"`
	Audit    []ModelImportAudit `json:"audit,omitempty"`
}
type ModelImportView struct {
	ChannelUID           string           `json:"channelUid"`
	Revision             string           `json:"revision"`
	State                ModelImportState `json:"state"`
	Configured           bool             `json:"configured"`
	UnrestrictedOnRemove bool             `json:"unrestrictedOnRemove"`
}

var ErrInvalidModelImport = errors.New("invalid model import")

type modelImportValidationError string

func (e modelImportValidationError) Error() string        { return string(e) }
func (e modelImportValidationError) Is(target error) bool { return target == ErrInvalidModelImport }
func invalidModelImport(message string) error             { return modelImportValidationError(message) }

func CloneModelImport(state *ModelImportState) *ModelImportState {
	if state == nil {
		return nil
	}
	raw, _ := json.Marshal(state)
	var clone ModelImportState
	_ = json.Unmarshal(raw, &clone)
	return &clone
}
func validImportedID(id string) bool {
	if id == "" || len(id) > 256 || strings.ContainsAny(id, "*!?#%\\") {
		return false
	}
	for _, segment := range strings.Split(id, "/") {
		if segment == "." || segment == ".." || segment == "" {
			return false
		}
	}
	for _, c := range id {
		if unicode.IsSpace(c) || unicode.IsControl(c) {
			return false
		}
	}
	return true
}
func compileImportRules(patterns []string) ([]*regexp.Regexp, error) {
	if len(patterns) > 32 {
		return nil, invalidModelImport("too many model import filters")
	}
	rules := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if len(p) == 0 || len(p) > 256 {
			return nil, invalidModelImport("invalid model import filter size")
		}
		r, err := regexp.Compile(p)
		if err != nil {
			return nil, invalidModelImport("invalid model import regular expression")
		}
		rules = append(rules, r)
	}
	return rules, nil
}
func ValidateModelImportPolicy(policy ModelImportPolicy) error {
	if policy.Case != "" && policy.Case != "lower" && policy.Case != "upper" {
		return invalidModelImport("unsupported model name case normalization")
	}
	if len(policy.StripPrefix) > 256 || len(policy.StripSuffix) > 256 || len(policy.Rules) > 16 {
		return invalidModelImport("model import normalization exceeds limits")
	}
	if _, err := compileImportRules(policy.Include); err != nil {
		return err
	}
	if _, err := compileImportRules(policy.Exclude); err != nil {
		return err
	}
	for _, rule := range policy.Rules {
		if len(rule.Pattern) == 0 || len(rule.Pattern) > 256 || len(rule.Replacement) > 256 {
			return invalidModelImport("invalid model import normalization rule")
		}
		if _, err := regexp.Compile(rule.Pattern); err != nil {
			return invalidModelImport("invalid model import normalization expression")
		}
	}
	return nil
}
func matchesImport(rules []*regexp.Regexp, id string) bool {
	for _, r := range rules {
		if r.MatchString(id) {
			return true
		}
	}
	return false
}
func NormalizeEndpointType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "chat", "openai", "chat_completions":
		return "chat"
	case "messages", "anthropic", "claude":
		return "messages"
	case "responses", "openai-response", "openai_response":
		return "responses"
	case "gemini", "google":
		return "gemini"
	case "images", "image":
		return "images"
	case "vectors", "embeddings", "embedding":
		return "vectors"
	}
	return ""
}
func normalizeImportEndpoints(endpoints []ModelEndpointMetadata, source string) []ModelEndpointMetadata {
	out := make([]ModelEndpointMetadata, 0)
	seen := make(map[string]bool)
	for _, ep := range endpoints {
		ep.Type = NormalizeEndpointType(ep.Type)
		if ep.Type == "" {
			continue
		}
		if source != "discovery" {
			ep.Source = "operator"
			ep.Verified = false
		} else if ep.Source != "probe" {
			ep.Verified = false
			if ep.Source != "provider_declared" {
				ep.Source = "configured"
			}
		}
		key := ep.Type + ":" + ep.Source
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ep)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type == out[j].Type {
			return out[i].Source < out[j].Source
		}
		return out[i].Type < out[j].Type
	})
	return out
}

// BuildModelImport filters original upstream IDs before normalization. Aliases
// never replace the actual provider ID; collisions reject the complete review.
func BuildModelImport(policy ModelImportPolicy, candidates []ModelImportCandidate, source string) ([]ImportedModel, error) {
	if err := ValidateModelImportPolicy(policy); err != nil {
		return nil, err
	}
	if source != "discovery" && source != "operator" {
		return nil, invalidModelImport("unknown model import source")
	}
	if len(candidates) > 8192 {
		return nil, invalidModelImport("upstream model inventory exceeds import limit")
	}
	includes, _ := compileImportRules(policy.Include)
	excludes, _ := compileImportRules(policy.Exclude)
	compiled := make([]*regexp.Regexp, len(policy.Rules))
	for i, r := range policy.Rules {
		compiled[i] = regexp.MustCompile(r.Pattern)
	}
	byRaw := make(map[string]ImportedModel)
	byAlias := make(map[string]string)
	for _, candidate := range candidates {
		raw := strings.TrimSpace(candidate.ID)
		if !validImportedID(raw) {
			return nil, invalidModelImport("invalid upstream model ID")
		}
		if (len(includes) > 0 && !matchesImport(includes, raw)) || matchesImport(excludes, raw) {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(raw, policy.StripPrefix), policy.StripSuffix)
		if policy.Case == "lower" {
			id = strings.ToLower(id)
		} else if policy.Case == "upper" {
			id = strings.ToUpper(id)
		}
		for i, r := range policy.Rules {
			id = compiled[i].ReplaceAllString(id, r.Replacement)
			if len(id) > 256 {
				return nil, invalidModelImport("model normalization output exceeds limit")
			}
		}
		if !validImportedID(id) {
			return nil, invalidModelImport("normalization produced an invalid model ID")
		}
		if other, ok := byAlias[id]; ok && other != raw {
			return nil, invalidModelImport("multiple upstream models normalize to the same ID")
		}
		byAlias[id] = raw
		ep := normalizeImportEndpoints(candidate.Endpoints, source)
		if previous, ok := byRaw[raw]; ok {
			ep = normalizeImportEndpoints(append(previous.Endpoints, ep...), source)
		}
		byRaw[raw] = ImportedModel{ID: id, UpstreamID: raw, Endpoints: ep}
		if len(byRaw) > MaxImportedModels {
			return nil, invalidModelImport("filtered model inventory exceeds import limit")
		}
	}
	// A public alias must not shadow another approved raw provider ID either.
	for raw := range byRaw {
		if aliasTarget, ok := byAlias[raw]; ok && aliasTarget != raw {
			return nil, invalidModelImport("public model alias conflicts with another upstream ID")
		}
	}
	out := make([]ImportedModel, 0, len(byRaw))
	for _, item := range byRaw {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func importDiff(before, after []ImportedModel) ModelImportDiff {
	old, next := make(map[string]ImportedModel), make(map[string]ImportedModel)
	for _, item := range before {
		old[item.ID] = item
	}
	for _, item := range after {
		next[item.ID] = item
	}
	diff := ModelImportDiff{Added: []string{}, Removed: []string{}, Changed: []string{}}
	for id, item := range next {
		if previous, ok := old[id]; !ok {
			diff.Added = append(diff.Added, id)
		} else if !reflect.DeepEqual(previous, item) {
			diff.Changed = append(diff.Changed, id)
		}
	}
	for id := range old {
		if _, ok := next[id]; !ok {
			diff.Removed = append(diff.Removed, id)
		}
	}
	sort.Strings(diff.Added)
	sort.Strings(diff.Removed)
	sort.Strings(diff.Changed)
	return diff
}
func modelImportRevision(channel *UpstreamConfig) string {
	copy := channel.Clone()
	if copy.ModelImport != nil {
		copy.ModelImport.Pending = nil
	}
	raw, _ := json.Marshal(copy)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (cm *ConfigManager) modelImportChannelLocked(uid string) *UpstreamConfig {
	for _, kind := range orderedChannelKinds() {
		slice := ChannelKindRegistry[kind].SliceRef(&cm.config)
		for i := range *slice {
			if (*slice)[i].ChannelUID == uid {
				return &(*slice)[i]
			}
		}
	}
	return nil
}
func modelImportView(channel *UpstreamConfig) ModelImportView {
	state := CloneModelImport(channel.ModelImport)
	if state == nil {
		state = &ModelImportState{}
	}
	return ModelImportView{ChannelUID: channel.ChannelUID, Revision: modelImportRevision(channel), State: *state, Configured: channel.ModelImport != nil, UnrestrictedOnRemove: len(channel.SupportedModels) == 0}
}
func (cm *ConfigManager) GetModelImport(uid string) (ModelImportView, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	channel := cm.modelImportChannelLocked(uid)
	if channel == nil {
		return ModelImportView{}, ErrModelImportNotFound
	}
	return modelImportView(channel), nil
}
func appendImportAudit(state *ModelImportState, actor, action, reviewID, operationID string) {
	state.Audit = append(state.Audit, ModelImportAudit{OperationID: operationID, At: time.Now().UTC(), ActorID: actor, Action: action, ReviewID: reviewID, ModelCount: len(state.Approved)})
	if len(state.Audit) > 100 {
		state.Audit = append([]ModelImportAudit(nil), state.Audit[len(state.Audit)-100:]...)
	}
}

// SetModelImportPolicy commits policy and audit atomically. An enabled policy
// with no approved models is closed, never the legacy empty=all fallback.
func (cm *ConfigManager) SetModelImportPolicyAudited(auditContext ManagementAuditContext, uid, expected string, policy ModelImportPolicy, actor string) (view ModelImportView, err error) {
	auditContext = auditContext.normalized()
	if actor == "" || len(actor) > 256 {
		return view, invalidModelImport("model import policy requires an authenticated actor")
	}
	if err = ValidateModelImportPolicy(policy); err != nil {
		return view, err
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	channel := cm.modelImportChannelLocked(uid)
	if channel == nil {
		return view, ErrModelImportNotFound
	}
	if expected == "" || strings.Trim(expected, "\"") != modelImportRevision(channel) {
		return view, ErrModelImportConflict
	}
	before := cm.config.deepCopy()
	defer func() {
		if err != nil {
			cm.config = before
		}
	}()
	state := CloneModelImport(channel.ModelImport)
	if state == nil {
		state = &ModelImportState{}
	}
	state.Policy = policy
	state = CloneModelImport(state)
	state.Pending = nil
	appendImportAudit(state, actor, "policy", "", auditContext.OperationID)
	channel.ModelImport = state
	if err = cm.saveConfigLockedAudited(auditContext, cm.config); err != nil {
		return view, err
	}
	return modelImportView(cm.modelImportChannelLocked(uid)), nil
}

// StageModelImport stores a review bound to the full current channel snapshot.
// Discovery only stages evidence; it never silently replaces an active catalog.
func (cm *ConfigManager) StageModelImportAudited(auditContext ManagementAuditContext, uid string, candidates []ModelImportCandidate, source string, expectedRevision ...string) (review ModelImportReview, err error) {
	auditContext = auditContext.normalized()
	cm.mu.Lock()
	defer cm.mu.Unlock()
	channel := cm.modelImportChannelLocked(uid)
	if channel == nil {
		return review, ErrModelImportNotFound
	}
	if channel.ModelImport == nil {
		return review, invalidModelImport("configure model import policy before preview")
	}
	if len(expectedRevision) > 0 && (expectedRevision[0] == "" || strings.Trim(expectedRevision[0], "\"") != modelImportRevision(channel)) {
		return review, ErrModelImportConflict
	}
	models, err := BuildModelImport(channel.ModelImport.Policy, candidates, source)
	if err != nil {
		return review, err
	}
	review = ModelImportReview{BaseRevision: modelImportRevision(channel), ObservedAt: time.Now().UTC(), Source: source, Models: models, Diff: importDiff(channel.ModelImport.Approved, models), PreviousSupportedModels: append([]string(nil), channel.SupportedModels...)}
	raw, _ := json.Marshal(review)
	sum := sha256.Sum256(raw)
	review.ID = hex.EncodeToString(sum[:])
	before := cm.config.deepCopy()
	defer func() {
		if err != nil {
			cm.config = before
		}
	}()
	state := CloneModelImport(channel.ModelImport)
	pendingCopy := review
	state.Pending = &pendingCopy
	channel.ModelImport = state
	if err = cm.saveConfigLockedAudited(auditContext, cm.config); err != nil {
		return ModelImportReview{}, err
	}
	return *CloneModelImport(cm.modelImportChannelLocked(uid).ModelImport).Pending, nil
}

// ApproveModelImport activates exactly the reviewed inventory, with explicit
// acknowledgement of empty/removal changes and rollback on persistence failure.
func (cm *ConfigManager) ApproveModelImportAudited(auditContext ManagementAuditContext, uid, reviewID, actor string, allowEmpty, allowRemovals bool) (view ModelImportView, err error) {
	auditContext = auditContext.normalized()
	if actor == "" || len(actor) > 256 {
		return view, invalidModelImport("model import approval requires an authenticated actor")
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	channel := cm.modelImportChannelLocked(uid)
	if channel == nil {
		return view, ErrModelImportNotFound
	}
	state := CloneModelImport(channel.ModelImport)
	if state == nil || state.Pending == nil || state.Pending.ID != reviewID || state.Pending.BaseRevision != modelImportRevision(channel) {
		return view, ErrModelImportConflict
	}
	if len(state.Pending.Models) == 0 && !allowEmpty {
		return view, invalidModelImport("empty model import requires acknowledgement")
	}
	if len(state.Pending.Diff.Removed) > 0 && !allowRemovals {
		return view, invalidModelImport("model removals require acknowledgement")
	}

	before := cm.config.deepCopy()
	defer func() {
		if err != nil {
			cm.config = before
		}
	}()
	state.Policy.Enabled = true
	state.Approved = state.Pending.Models
	state.Pending = nil
	appendImportAudit(state, actor, "approve", reviewID, auditContext.OperationID)
	channel.ModelImport = state
	channel.SupportedModels = make([]string, 0, len(state.Approved))
	for _, item := range state.Approved {
		channel.SupportedModels = append(channel.SupportedModels, item.UpstreamID)
	}
	if err = cm.saveConfigLockedAudited(auditContext, cm.config); err != nil {
		return view, err
	}
	return modelImportView(cm.modelImportChannelLocked(uid)), nil
}

// ImportedModel resolves an approved public alias or raw provider ID. It does
// not infer aliases from model families and cannot grant a key upstream access.
func (u *UpstreamConfig) ImportedModel(model string) (ImportedModel, bool) {
	if u == nil || u.ModelImport == nil || !u.ModelImport.Policy.Enabled {
		return ImportedModel{}, false
	}
	for _, item := range u.ModelImport.Approved {
		if item.ID == model || item.UpstreamID == model {
			return item, true
		}
	}
	return ImportedModel{}, false
}
func (u *UpstreamConfig) ModelImportEnabled() bool {
	return u != nil && u.ModelImport != nil && u.ModelImport.Policy.Enabled
}
func (u *UpstreamConfig) CheckImportedModel(model string) (string, error) {
	if !u.ModelImportEnabled() {
		return model, nil
	}
	item, ok := u.ImportedModel(model)
	if !ok {
		return "", invalidModelImport("model is outside the approved import catalog")
	}
	return item.UpstreamID, nil
}

// ModelImportPolicyOnly carries operator intent to a new channel, never proof or
// approval bound to another channel/key/instance. Offline state bundles retain
// full state; generic channel creation and share packs must not transfer it.
func ModelImportPolicyOnly(state *ModelImportState) *ModelImportState {
	if state == nil {
		return nil
	}
	clone := CloneModelImport(state)
	return &ModelImportState{Policy: clone.Policy}
}
func ValidateNewChannelModelImport(state *ModelImportState) error {
	if state == nil {
		return nil
	}
	if len(state.Approved) > 0 || state.Pending != nil || len(state.Audit) > 0 {
		return invalidModelImport("model import approvals and evidence are server-owned; use policy and review endpoints")
	}
	return ValidateModelImportPolicy(state.Policy)
}

// ModelImportRevision binds discovery evidence to its source configuration.
// Pending proposals are excluded so writing a review cannot invalidate itself.
func ModelImportRevision(channel *UpstreamConfig) string { return modelImportRevision(channel) }

// ModelImportInventoryRevision prevents a pre-approval /models cache entry from
// reappearing after policy activation/removal when an upstream is unavailable.
func (cm *ConfigManager) ModelImportInventoryRevision() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	type inventory struct {
		UID     string
		Enabled bool
		Models  []ImportedModel
	}
	entries := make([]inventory, 0)
	for _, kind := range orderedChannelKinds() {
		for _, up := range ChannelKindRegistry[kind].Slice(cm.config) {
			if up.ModelImport != nil {
				entries = append(entries, inventory{up.ChannelUID, up.ModelImport.Policy.Enabled, up.ModelImport.Approved})
			}
		}
	}
	if len(entries) == 0 {
		return ""
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].UID < entries[j].UID })
	raw, _ := json.Marshal(entries)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// RemoveModelImport explicitly restores legacy automatic configuration writes.
// Keep the current supported-model rules; broadening an empty list to legacy
// unrestricted admission requires a separate acknowledgement.
func (cm *ConfigManager) RemoveModelImportAudited(auditContext ManagementAuditContext, uid, expected, actor string, allowUnrestricted bool) (view ModelImportView, err error) {
	auditContext = auditContext.normalized()
	if actor == "" || len(actor) > 256 {
		return view, invalidModelImport("model import removal requires an authenticated actor")
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	channel := cm.modelImportChannelLocked(uid)
	if channel == nil {
		return view, ErrModelImportNotFound
	}
	if expected == "" || strings.Trim(expected, "\"") != modelImportRevision(channel) {
		return view, ErrModelImportConflict
	}
	if len(channel.SupportedModels) == 0 && !allowUnrestricted {
		return view, invalidModelImport("restoring unrestricted legacy models requires acknowledgement")
	}
	before := cm.config.deepCopy()
	defer func() {
		if err != nil {
			cm.config = before
		}
	}()
	channel.ModelImport = nil
	if err = cm.saveConfigLockedAudited(auditContext, cm.config); err != nil {
		return view, err
	}
	return modelImportView(cm.modelImportChannelLocked(uid)), nil
}

func (cm *ConfigManager) SetModelImportPolicy(uid, expected string, policy ModelImportPolicy, actor string) (view ModelImportView, err error) {
	return cm.SetModelImportPolicyAudited(ManagementAuditContext{}, uid, expected, policy, actor)
}

func (cm *ConfigManager) StageModelImport(uid string, candidates []ModelImportCandidate, source string, expectedRevision ...string) (review ModelImportReview, err error) {
	return cm.StageModelImportAudited(ManagementAuditContext{}, uid, candidates, source, expectedRevision...)
}

func (cm *ConfigManager) ApproveModelImport(uid, reviewID, actor string, allowEmpty, allowRemovals bool) (view ModelImportView, err error) {
	return cm.ApproveModelImportAudited(ManagementAuditContext{}, uid, reviewID, actor, allowEmpty, allowRemovals)
}

func (cm *ConfigManager) RemoveModelImport(uid, expected, actor string, allowUnrestricted bool) (view ModelImportView, err error) {
	return cm.RemoveModelImportAudited(ManagementAuditContext{}, uid, expected, actor, allowUnrestricted)
}
