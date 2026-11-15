package features

const (
	NativeFoundation = "foundation"
	NativeManual     = "manual"
	NativeExcluded   = "excluded"
)

// NativeUsageForPlatform separates consumers without changing protocol support,
// existing IDs, manual operations or historical acceptance.
func NativeUsageForPlatform(platform string) map[string]string {
	out := map[string]string{}
	for _, item := range NativeCatalogForPlatform(platform) {
		out[item.ID] = nativeUsage(item.ID)
	}
	for _, detail := range NativeDetailsForPlatform(platform) {
		out[detail.ID] = out[detail.ParentID]
		if detail.ID == "send_file" {
			out[detail.ID] = NativeExcluded
		}
	}
	return out
}

// NativeBusinessAllowed is a consumer boundary, not execution admission.
// Manual account/media operations must never become business or assistant tools,
// even if their own native acceptance subsequently passes.
func NativeBusinessAllowed(platform, id string) bool {
	if id == "send_file" {
		return false
	}
	for _, item := range NativeCatalogForPlatform(platform) {
		if item.ID == id {
			return nativeUsage(id) == NativeFoundation
		}
	}
	for _, detail := range NativeDetailsForPlatform(platform) {
		if detail.ID == id {
			return nativeUsage(detail.ParentID) == NativeFoundation
		}
	}
	return false
}

func nativeUsage(parent string) string {
	switch parent {
	case "account_signup", "account_login", "account_settings", "approve_friend", "friend_delete", "onboarding_friend_request", "read_voice", "read_file":
		return NativeManual
	}
	return NativeFoundation
}
