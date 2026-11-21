package config

import (
	"encoding/json"
	"testing"
)

func TestCopilotProtocolCRUDAndSnapshot(t *testing.T) {
	cm := newTestConfigManager(t, `{"responsesUpstream":[]}`)
	if err := cm.AddResponsesUpstream(UpstreamConfig{ServiceType: "copilot", CopilotProtocol: "chat", BaseURL: "https://example.invalid", APIKeys: []string{"github-fake"}}); err != nil {
		t.Fatal(err)
	}
	first := cm.GetConfig().ResponsesUpstream[0]
	if first.ServiceType != "copilot" || first.Clone().CopilotProtocol != "chat" {
		t.Fatalf("identity/clone=%+v", first)
	}
	protocol := "messages"
	if _, err := cm.UpdateResponsesUpstream(0, UpstreamUpdate{CopilotProtocol: &protocol}); err != nil {
		t.Fatal(err)
	}
	// A normal edit omits the field and must preserve the authenticated route's wire setting.
	remark := "edited"
	if _, err := cm.UpdateResponsesUpstream(0, UpstreamUpdate{Remark: &remark}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(cm.GetConfig())
	if err != nil {
		t.Fatal(err)
	}
	var restored Config
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.ResponsesUpstream[0].CopilotProtocol != "messages" || restored.ResponsesUpstream[0].ServiceType != "copilot" {
		t.Fatal("JSON/save lost protocol or identity")
	}
	invalid := "https://example.invalid/responses"
	if _, err := cm.UpdateResponsesUpstream(0, UpstreamUpdate{CopilotProtocol: &invalid}); err == nil {
		t.Fatal("accepted arbitrary endpoint as protocol")
	}
	if cm.GetConfig().ResponsesUpstream[0].CopilotProtocol != "messages" {
		t.Fatal("invalid update mutated route")
	}
}
