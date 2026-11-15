package nativeauth

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestOnboardingInfoIdentityReasonsPreserveStrictValidation(t *testing.T) {
	for _, field := range []string{"uid", "accountId"} {
		prefix, matching, conflicting := "friend_info_uid_", "8", "9"
		if field == "accountId" {
			prefix, matching, conflicting = "friend_info_account_", "340738649", "340738650"
		}
		for _, tc := range []struct {
			name, raw, reason string
		}{
			{"missing", "", "missing"},
			{"null", "null", "null"},
			{"number_zero", "0", "zero"},
			{"string_zero", `"0"`, "zero"},
			{"spaced_zero", " \n0\t", "zero"},
			{"negative", "-1", "invalid"},
			{"fraction", "0.0", "invalid"},
			{"exponent", "0e0", "invalid"},
			{"noncanonical_zero", `"00"`, "invalid"},
			{"empty_string", `""`, "invalid"},
			{"text", `"synthetic-private-identity"`, "invalid"},
			{"object", `{"uid":"synthetic-private-identity"}`, "invalid"},
			{"array", "[]", "invalid"},
			{"boolean", "false", "invalid"},
			{"overflow", "18446744073709551616", "invalid"},
			{"malformed", "{", "invalid"},
			{"number_conflict", conflicting, "mismatch"},
			{"string_conflict", `"` + conflicting + `"`, "mismatch"},
			{"number_match", matching, ""},
			{"string_match", `"` + matching + `"`, ""},
		} {
			for _, mobile := range []bool{false, true} {
				mode := "desktop"
				if mobile {
					mode = "mobile"
				}
				t.Run(field+"/"+tc.name+"/"+mode, func(t *testing.T) {
					info := onboardingOutboundData(t, onboardingOutboundInfoRow())
					if tc.raw == "" {
						delete(info, field)
					} else {
						info[field] = json.RawMessage(tc.raw)
					}
					target := OnboardingOutboundTarget{ID: "8", Account: "340738649"}
					got, err := parseOnboardingOutboundInfo(info, target, "CONVERSATION_RELATION_STRANGER_USER", mobile)
					if tc.reason == "" || mobile && field == "accountId" && tc.reason == "zero" {
						if err != nil || got.ID != target.ID || got.Account != target.Account || !got.CanRequest {
							t.Fatal("valid identity or existing mobile default changed", got, err)
						}
						return
					}
					var lookup *OnboardingLookupError
					var native *Error
					if got != (OnboardingOutboundTarget{}) || !errors.As(err, &lookup) ||
						lookup.Stage != "friend_info" || lookup.Reason != prefix+tc.reason ||
						!errors.As(err, &native) || native.Code != "native_onboarding_outbound_changed" || native.Submitted {
						t.Fatal("identity rejection lost its bounded reason or strict denial", err)
					}
					if strings.Contains(err.Error(), "synthetic-private") {
						t.Fatal("identity content escaped error text")
					}
				})
			}
		}
	}
}

func TestOnboardingInfoIdentityReasonChecksUIDBeforeAccount(t *testing.T) {
	info := onboardingOutboundData(t, onboardingOutboundInfoRow())
	delete(info, "uid")
	info["accountId"] = json.RawMessage("340738650")
	_, err := parseOnboardingOutboundInfo(info, OnboardingOutboundTarget{ID: "8", Account: "340738649"},
		"CONVERSATION_RELATION_STRANGER_USER", false)
	var lookup *OnboardingLookupError
	if !errors.As(err, &lookup) || lookup.Reason != "friend_info_uid_missing" {
		t.Fatal("account diagnosis preceded an exact UID match", err)
	}
}

func TestOnboardingLookupIdentityReasonWhitelistAndWrapping(t *testing.T) {
	for _, field := range []string{"uid", "account"} {
		for _, state := range []string{"missing", "null", "zero", "invalid", "mismatch"} {
			reason := "friend_info_" + field + "_" + state
			cause := onboardingOutboundIdentityError(reason)
			wrapped := onboardingLookupError("friend_info", cause)
			var native *Error
			if SafeOnboardingLookupReason(reason) != reason || wrapped.Reason != reason ||
				!errors.As(wrapped, &native) || native.Code != "native_onboarding_outbound_changed" || native.Submitted {
				t.Fatal("identity reason or native rejection lost during lookup wrapping")
			}
			if onboardingLookupError("search", cause).Reason != "" {
				t.Fatal("identity diagnosis was relabelled as a search failure")
			}
		}
	}
	for _, reason := range []string{"", "friend_info_uid_match", "uid_zero",
		"friend_info_account_zero synthetic-private-identity", strings.Repeat("x", 1024)} {
		wrapped := onboardingLookupError("friend_info", &OnboardingLookupError{
			Stage: "friend_info", Reason: reason, Cause: rejection("native_onboarding_outbound_changed")})
		if SafeOnboardingLookupReason(reason) != "" || wrapped.Reason != "" {
			t.Fatal("unreviewed identity reason crossed the whitelist")
		}
	}
}
