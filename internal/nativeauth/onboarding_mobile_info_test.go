package nativeauth

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestOnboardingMobileDefaultAccountKeepsExactSearchIdentity(t *testing.T) {
	for _, value := range []any{0, "0"} {
		data := onboardingOutboundData(t, map[string]any{"items": []any{onboardingOutboundSearchRow()}})
		target, relation, err := parseOnboardingOutboundSearch(data, "7", "340738649")
		if err != nil {
			t.Fatal(err)
		}
		info := onboardingOutboundInfoRow()
		info["accountId"] = value
		got, err := parseOnboardingOutboundInfo(onboardingOutboundData(t, info), target, relation, true)
		if err != nil || got.ID != target.ID || got.Account != target.Account || !got.CanRequest {
			t.Fatal("mobile default account lost its exact search binding", err)
		}
		if _, err := parseOnboardingOutboundInfo(onboardingOutboundData(t, info), target, relation, false); err == nil {
			t.Fatal("mobile default account weakened desktop identity validation")
		}
	}
}

func TestOnboardingMobileDefaultAccountRejectsUnboundOrConflictingIdentity(t *testing.T) {
	for _, mode := range []string{"wrong_uid", "missing_uid", "null_uid", "wrong_account",
		"missing_account", "null_account", "negative_account", "float_account", "exponent_account",
		"noncanonical_zero", "bad_search_uid", "bad_search_account", "missing_state", "null_policy"} {
		t.Run(mode, func(t *testing.T) {
			target := OnboardingOutboundTarget{ID: "8", Account: "340738649"}
			info := onboardingOutboundData(t, onboardingOutboundInfoRow())
			info["accountId"] = json.RawMessage("0")
			switch mode {
			case "wrong_uid":
				info["uid"] = json.RawMessage("9")
			case "missing_uid":
				delete(info, "uid")
			case "null_uid":
				info["uid"] = json.RawMessage("null")
			case "wrong_account":
				info["accountId"] = json.RawMessage("340738650")
			case "missing_account":
				delete(info, "accountId")
			case "null_account":
				info["accountId"] = json.RawMessage("null")
			case "negative_account":
				info["accountId"] = json.RawMessage("-1")
			case "float_account":
				info["accountId"] = json.RawMessage("0.0")
			case "exponent_account":
				info["accountId"] = json.RawMessage("0e0")
			case "noncanonical_zero":
				info["accountId"] = json.RawMessage(`"00"`)
			case "bad_search_uid":
				target.ID = "0"
			case "bad_search_account":
				target.Account = "0"
			case "missing_state":
				delete(info, "state1")
			case "null_policy":
				info["friendPolicy"] = json.RawMessage("null")
			}
			got, err := parseOnboardingOutboundInfo(info, target, "CONVERSATION_RELATION_STRANGER_USER", true)
			if err == nil || got != (OnboardingOutboundTarget{}) {
				t.Fatal("mobile default account admitted unverified evidence")
			}
		})
	}
}

func TestOnboardingIncompleteRelationshipIsNotReportedAsScopeChange(t *testing.T) {
	info := onboardingOutboundInfoRow()
	info["state"], info["state1"] = "FRIEND_STATE_GOOD", "FRIEND_STATE_DELETED"
	got, err := parseOnboardingOutboundInfo(onboardingOutboundData(t, info),
		OnboardingOutboundTarget{ID: "8", Account: "340738649"}, "CONVERSATION_RELATION_STRANGER_USER", true)
	if err != nil || got.CanRequest || got.IsFriend || got.BlockedReason != "native_onboarding_outbound_read_incomplete" {
		t.Fatal("one-sided relationship became request permission or scope failure")
	}
	info["uid"] = 9
	_, err = parseOnboardingOutboundInfo(onboardingOutboundData(t, info),
		OnboardingOutboundTarget{ID: "8", Account: "340738649"}, "CONVERSATION_RELATION_STRANGER_USER", true)
	var native *Error
	if !errors.As(err, &native) || native.Code != "native_onboarding_outbound_changed" {
		t.Fatal("actual target substitution lost its denial")
	}
}
