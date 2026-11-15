package features

import (
	"slices"
	"testing"
)

func TestBusinessActionSourcesMatchImplementedConsumers(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, action := range BusinessActions(platform) {
			want := []string{"assistant", "manual"}
			switch action.ID {
			case "send_text":
				want = append(want, "automatic", "heatbot")
			case "recall_message", "mute_member":
				want = append(want, "automatic")
			case "group_accept":
				want = []string{"onboarding"}
			}
			if !slices.Equal(action.Sources, want) {
				t.Fatalf("%s/%s sources %v, want %v", platform, action.ID, action.Sources, want)
			}
			for _, source := range []string{"assistant", "manual", "automatic", "heatbot", "onboarding", "native_debug"} {
				_, ok := FindBusinessAction(platform, action.ID, source)
				if ok != slices.Contains(want, source) {
					t.Fatalf("%s/%s admitted unexpected source %s", platform, action.ID, source)
				}
			}
		}
	}
}
