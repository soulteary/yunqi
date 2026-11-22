package presetstore

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"
)

type priceLogFailureTransport struct{}

func (priceLogFailureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("opaque-log-transport-private-value: %s", request.URL.String())
}
func TestPriceSourceBackgroundErrorsUseControlledStatusAndLogs(t *testing.T) {
	const source = "https://private-log-user:private-log-password@example.invalid/index.json?hint=private-log-query#private-log-fragment"
	updater := NewPresetUpdater(NewPresetStore(nil), UpdaterConfig{Enabled: true, IndexURL: source, CacheDir: t.TempDir(), HTTPClient: &http.Client{Transport: priceLogFailureTransport{}}, Interval: time.Hour})
	old := log.Writer()
	var output bytes.Buffer
	log.SetOutput(&output)
	updater.Start(context.Background())
	deadline := time.Now().Add(2 * time.Second)
	for updater.Status().LastCheckAt == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	updater.Stop()
	log.SetOutput(old)
	if updater.Status().LastError != "source_check_failed" {
		t.Fatal("background check did not retain a controlled error")
	}
	text := output.String()
	for _, secret := range []string{"private-log-user", "private-log-password", "private-log-query", "private-log-fragment", "opaque-log-transport-private-value"} {
		if strings.Contains(text, secret) {
			t.Fatalf("background source error disclosed secret: %s", text)
		}
	}
	if !strings.Contains(text, "source_check_failed") {
		t.Fatal("background failure did not log a controlled summary")
	}
}
