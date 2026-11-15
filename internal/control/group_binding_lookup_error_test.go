package control

import (
	"testing"
	"time"
)

func TestGroupBindingLookupIdentityReasonsAreBoundedAndScoped(t *testing.T) {
	for _, field := range []string{"uid", "account"} {
		for _, state := range []string{"missing", "null", "zero", "invalid", "mismatch"} {
			reason := "friend_info_" + field + "_" + state
			base := GroupBindingLookupDiagnostic{Version: 1, Platform: "wsl",
				Stage: "friend_info", Category: "response_invalid", Reason: reason, ObservedAt: time.Now()}
			got, ok := SafeGroupBindingLookupDiagnostic(base)
			if !ok || got.Reason != reason {
				t.Fatal("fixed identity reason was lost")
			}
			for _, mode := range []string{"other_platform", "other_stage", "other_category", "private_reason"} {
				changed := base
				switch mode {
				case "other_platform":
					changed.Platform = "lajiaoliao"
				case "other_stage":
					changed.Stage = "search"
				case "other_category":
					changed.Category = "upstream_rejected"
				case "private_reason":
					changed.Reason += " synthetic-private-identity"
				}
				if got, ok := SafeGroupBindingLookupDiagnostic(changed); !ok || got.Reason != "" {
					t.Fatal("identity reason escaped its fixed WSL friend-info context", mode)
				}
			}
		}
	}
}
