package features

import (
	"strings"
	"testing"
)

func TestOutgoingFriendRemainsControlledOnlyWithoutBusinessConsumers(t *testing.T) {
	const id = "onboarding_friend_request"
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		found := false
		for _, item := range NativeCatalogForPlatform(platform) {
			if item.ID == id {
				found = strings.Contains(item.Name, "仅超管受控测试") && strings.Contains(item.Summary, "410")
			}
		}
		if !found || NativeUsageForPlatform(platform)[id] != NativeManual || NativeBusinessAllowed(platform, id) {
			t.Fatalf("%s outgoing capability lost its ID or controlled boundary", platform)
		}
		for _, d := range CatalogForPlatform(platform) {
			for _, dependency := range d.NativeDependencies {
				if dependency == id {
					t.Fatalf("%s/%s consumes controlled outgoing contact", platform, d.ID)
				}
			}
		}
		for _, action := range BusinessActions(platform) {
			if action.ID == id || action.NativeID == id {
				t.Fatalf("%s business action admits controlled outgoing contact", platform)
			}
		}
	}
}
