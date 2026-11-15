package httpapi

import (
	"testing"
	"time"
)

func TestExtendedEndpointTimeoutIsExactAndBounded(t *testing.T) {
	if got := extendedEndpointTimeout("POST /api/v1/ai-gateway/test"); got != 55*time.Second {
		t.Fatal("gateway test deadline", got)
	}
	if got := extendedEndpointTimeout("POST /api/v1/platform-assistant/sessions/{id}/turns"); got != 55*time.Second {
		t.Fatal("platform assistant deadline", got)
	}
	for _, pattern := range []string{
		"GET /api/v1/assistant/turns/{id}/events",
		"GET /api/v1/accounts/{id}/events",
		"GET /api/v1/system/monitor/events",
	} {
		if got := extendedEndpointTimeout(pattern); got != 100*time.Second {
			t.Fatalf("%s: %s", pattern, got)
		}
	}
	if got := extendedEndpointTimeout("POST /api/v1/accounts/{id}/lajiaoliao-login"); got != 45*time.Second {
		t.Fatalf("manual login chain budget: %s", got)
	}
	for _, pattern := range []string{
		"GET /api/v1/accounts", "POST /api/v1/accounts/{id}/lajiaoliao-challenge",
		"GET /api/v1/accounts/{id}/lajiaoliao-login",
		"POST /api/v1/accounts/{id}/native-auth", "POST /api/v1/assistant/turns",
	} {
		if got := extendedEndpointTimeout(pattern); got != 0 {
			t.Fatalf("ordinary route budget changed: %s: %s", pattern, got)
		}
	}
}
