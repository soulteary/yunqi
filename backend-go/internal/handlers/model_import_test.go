package handlers

import (
	"github.com/soulteary/yunqi/internal/config"
	"testing"
)

func TestModelImportShareExportNeverTransfersApprovalEvidence(t *testing.T) {
	source := config.UpstreamConfig{APIKeys: []string{"provider-secret"}, ModelImport: &config.ModelImportState{Policy: config.ModelImportPolicy{Enabled: true, Include: []string{"^raw$"}}, Approved: []config.ImportedModel{{ID: "public", UpstreamID: "raw", Endpoints: []config.ModelEndpointMetadata{{Type: "chat", Source: "probe", Verified: true}}}}, Pending: &config.ModelImportReview{ID: "review"}, Audit: []config.ModelImportAudit{{ActorID: "actor"}}}}
	exported := sanitizeForExport(source, false)
	if exported.ModelImport == nil || !exported.ModelImport.Policy.Enabled || len(exported.ModelImport.Approved) > 0 || exported.ModelImport.Pending != nil || len(exported.ModelImport.Audit) > 0 || len(exported.APIKeys) > 0 {
		t.Fatalf("server-owned state exported %+v", exported)
	}
	exported.ModelImport.Policy.Include[0] = "changed"
	if source.ModelImport.Policy.Include[0] != "^raw$" || len(source.ModelImport.Approved) != 1 {
		t.Fatal("share export mutated source")
	}
}
