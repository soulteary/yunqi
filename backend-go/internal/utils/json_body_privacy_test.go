package utils

import (
	"github.com/soulteary/yunqi/internal/redaction"
	"testing"
)

func TestDiagnosticHeadersHideWholeSecretsAndPreservePublicFields(t *testing.T) {
	source := map[string]string{"Authorization": "Bearer opaque-private-value", "Cookie": "session=private-cookie", "Set-Cookie": "session=new-private-cookie", "X-Custom-Secret": "private-header", "X-Custom-Auth": "private-opaque-auth", "X-Access-Token": "private-token", "X-Request-ID": "public-trace"}
	masked := MaskSensitiveHeaders(source)
	if masked["Authorization"] != "Bearer "+redaction.Mask {
		t.Fatal("authorization was partially exposed")
	}
	for _, name := range []string{"Cookie", "Set-Cookie", "X-Custom-Secret", "X-Access-Token", "X-Custom-Auth"} {
		if masked[name] != redaction.Mask {
			t.Fatalf("secret header %s was exposed", name)
		}
	}
	if masked["X-Request-ID"] != "public-trace" || source["Cookie"] != "session=private-cookie" {
		t.Fatal("diagnostic masking changed public metadata or request headers")
	}
}
