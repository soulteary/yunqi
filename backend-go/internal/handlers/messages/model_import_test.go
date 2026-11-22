package messages

import (
	"github.com/soulteary/yunqi/internal/config"
	"testing"
)

func TestModelImportModelsListUsesApprovedAliasesAndHonestEndpoints(t *testing.T) {
	up := &config.UpstreamConfig{ServiceType: "openai", ModelImport: &config.ModelImportState{Policy: config.ModelImportPolicy{Enabled: true}, Approved: []config.ImportedModel{{ID: "public", UpstreamID: "provider/raw", Endpoints: []config.ModelEndpointMetadata{{Type: "chat", Source: "probe", Verified: true}, {Type: "responses", Source: "provider_declared"}}}}}}
	result := enrichModelsForUpstream([]ModelEntry{{ID: "provider/raw"}, {ID: "unreviewed"}}, up, nil)
	if len(result) != 1 || result[0].ID != "public" || len(result[0].SupportedEndpointTypes) != 1 || result[0].SupportedEndpointTypes[0] != "chat" || len(result[0].EndpointMetadata) != 2 {
		t.Fatalf("catalog list exposed unapproved models or fabricated proof %+v", result)
	}
	up.ModelImport = nil
	result = enrichModelsForUpstream([]ModelEntry{{ID: "unreviewed", SupportedEndpointTypes: []string{"openai-response"}, EndpointMetadata: []config.ModelEndpointMetadata{{Type: "messages", Source: "probe", Verified: true}}}}, up, nil)
	if len(result[0].SupportedEndpointTypes) != 0 {
		t.Fatal("upstream self-report became verified endpoint metadata")
	}
	for _, metadata := range result[0].EndpointMetadata {
		if metadata.Verified || metadata.Source != "provider_declared" {
			t.Fatalf("forged supplier proof %+v", metadata)
		}
	}
}
