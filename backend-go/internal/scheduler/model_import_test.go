package scheduler

import (
	"context"
	"github.com/soulteary/yunqi/internal/config"
	"testing"
)

func TestModelImportResolverOnlyAdmitsApprovedActualModel(t *testing.T) {
	up := &config.UpstreamConfig{SupportedModels: []string{"provider/raw"}, ModelImport: &config.ModelImportState{Policy: config.ModelImportPolicy{Enabled: true}, Approved: []config.ImportedModel{{ID: "public", UpstreamID: "provider/raw"}}}}
	s := &ChannelScheduler{}
	actual := "provider/raw"
	calls := 0
	s.SetModelSupportResolverProvider(func(context.Context, ChannelKind, *config.UpstreamConfig, string) (bool, string, string, string) {
		calls++
		return true, actual, "profile", ""
	})
	if ok, why := s.resolveModelSupport(context.Background(), ChannelKindChat, up, "task-virtual"); !ok {
		t.Fatalf("approved task resolution refused: %s", why)
	}
	actual = "unreviewed"
	if ok, _ := s.resolveModelSupport(context.Background(), ChannelKindChat, up, "task-virtual"); ok {
		t.Fatal("unreviewed actual model accepted")
	}
	before := calls
	if ok, why := s.resolveModelSupport(context.Background(), ChannelKindChat, up, "public"); !ok {
		t.Fatalf("reviewed exact alias remapped: %s", why)
	}
	if calls != before {
		t.Fatal("family resolver overrode exact reviewed alias")
	}
}
