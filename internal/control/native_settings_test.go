package control

import (
	"errors"
	"testing"

	"wangshangliao/internal/nativeauth"
)

func TestNativeSettingRoleHierarchy(t *testing.T) {
	for _, row := range []struct {
		uid, target, role, targetRole string
		locked, allowed               bool
	}{
		{"7", "7", "member", "member", false, true},
		{"7", "8", "member", "member", false, false},
		{"7", "8", "admin", "member", false, true},
		{"7", "8", "admin", "admin", false, false},
		{"7", "8", "admin", "owner", false, false},
		{"7", "8", "owner", "admin", false, true},
		{"7", "7", "owner", "owner", true, false},
		{"7", "8", "unknown", "member", false, false},
	} {
		if got := settingRoleAllowed(row.uid, row.target, row.role, row.targetRole, row.locked); got != row.allowed {
			t.Fatal(row, got)
		}
	}
}

func TestNativeSettingOutcomes(t *testing.T) {
	for _, row := range []struct {
		err       error
		attempted bool
		status    string
	}{
		{nil, true, "succeeded"},
		{Denied(403, "revoked"), false, "failed"},
		{&nativeauth.Error{Code: "native_transport_unknown", Submitted: true}, false, "failed"},
		{&nativeauth.Error{Code: "native_transport_unknown", Submitted: true}, true, "unknown"},
		{&nativeauth.Error{Code: "native_business_rejected", Submitted: true}, true, "failed"},
		{errors.New("commit lost"), true, "unknown"},
	} {
		got, _ := settingOutcome(row.err, row.attempted)
		if got != row.status {
			t.Fatal(row, got)
		}
	}
}
