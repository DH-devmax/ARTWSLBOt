package features

import "testing"

func TestFriendDeleteStaysManualOnBothPlatforms(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		if NativeUsageForPlatform(platform)["friend_delete"] != NativeManual ||
			NativeBusinessAllowed(platform, "friend_delete") {
			t.Fatal("controlled deletion leaked into business consumers", platform)
		}
	}
}
