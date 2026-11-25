package capturedlimits

import (
	"strings"
	"testing"
)

func TestExplicitZeroTemplateRequest(t *testing.T) {
	const raw = `{"workloadUID":"a","fallbackRequestsUntil":"2030-01-01T00:00:00Z","containers":{"app":{"field":"containers","template":{},"limits":{},"templateRequests":{"cpu":"0"},"runningRequests":{"cpu":"15m"}}}}`
	if _, err := Parse(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(strings.Replace(raw, `"cpu":"0"`, `"cpu":"-1m"`, 1)); err == nil {
		t.Fatal("accepted negative template request")
	}
}
