package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

func TestBindingContactAdapterKeepsSealedDecisionAndTimestamp(t *testing.T) {
	now := time.Now().UTC()
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		input := control.GroupBindingFriendApproval{
			GroupBindingFriendRequest: control.GroupBindingFriendRequest{
				GroupBindingNativeScope: control.GroupBindingNativeScope{Platform: platform},
				Contact: &control.GroupBindingFriendSelector{
					Name: "Synthetic applicant", Since: now, Until: now.Add(time.Minute), RejectExpired: true,
				},
			},
			ApplicationID: "original-application", SenderID: "original-sender", RecipientID: "original-receiver",
			RequestedAt: now.Add(-24 * time.Hour), RejectExpired: true,
		}
		approval := bindingFriendApproval(input)
		if approval.Platform != platform || !approval.RejectExpired ||
			!approval.RequestedAt.Equal(input.RequestedAt) || approval.Contact == nil ||
			!approval.Contact.RejectExpired || !approval.Contact.IsExpired(nativeauth.OnboardingFriendEvidence{
			Name: input.Contact.Name, RequestedAt: approval.RequestedAt,
		}) {
			t.Fatal("contact adapter lost the sealed rejection or its original application time")
		}
		input.Contact.Name = "modified after conversion"
		if approval.Contact.Name != "Synthetic applicant" {
			t.Fatal("contact adapter shared mutable selector state")
		}
	}
}

func TestBindingFriendReadErrorsStayActionableAndPrivate(t *testing.T) {
	for _, prefix := range []string{"native_", "lajiaoliao_"} {
		for suffix, expected := range map[string]struct {
			status int
			code   string
		}{
			"not_found": {409, "group_binding_friend_request_not_found"},
			"ambiguous": {409, "group_binding_friend_request_ambiguous"},
			"shape":     {502, "group_binding_friend_response_invalid"},
			"changed":   {409, "group_binding_friend_application_changed"},
			"expired":   {409, "group_binding_challenge_expired"},
		} {
			var upstream error
			code := prefix + "onboarding_friend_" + suffix
			if prefix == "native_" {
				upstream = &nativeauth.Error{Code: code}
			} else {
				upstream = &lajiaoliao.Failure{Code: code}
			}
			err := bindingFriendReadError(fmt.Errorf("sensitive synthetic note: %w", upstream))
			var failure *control.Failure
			if !errors.As(err, &failure) || failure.Status != expected.status || failure.Code != expected.code {
				t.Fatalf("known friend read error not classified: %s", code)
			}
			w := httptest.NewRecorder()
			sendError(w, err)
			var response struct {
				Msg string `json:"msg"`
			}
			if json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Msg != expected.code ||
				strings.Contains(w.Body.String(), "sensitive") {
				t.Fatal("friend diagnosis leaked arbitrary upstream text")
			}
		}
	}
	for _, code := range []string{"native_social_shape", "native_social_limit_or_shape"} {
		var failure *control.Failure
		err := bindingFriendReadError(&nativeauth.Error{Code: code})
		if !errors.As(err, &failure) || failure.Status != 502 || failure.Code != "group_binding_friend_response_invalid" {
			t.Fatalf("friend list validation error not classified: %s", code)
		}
	}
	original := control.Denied(403, "group_binding_pool_identity")
	if bindingFriendReadError(original) != original {
		t.Fatal("friend diagnosis replaced an authorization failure")
	}
	unreviewed := &nativeauth.Error{Code: "synthetic-sensitive-upstream-code"}
	w := httptest.NewRecorder()
	sendError(w, bindingFriendReadError(unreviewed))
	if w.Code != 500 || strings.Contains(w.Body.String(), "synthetic-sensitive") {
		t.Fatal("unreviewed upstream code escaped the fixed diagnosis")
	}
}

func TestBindingContactReadErrorsStayActionableAndPrivate(t *testing.T) {
	w := httptest.NewRecorder()
	sendError(w, bindingFriendReadError(fmt.Errorf("synthetic private nickname and time: %w",
		&nativeauth.Error{Code: "native_onboarding_contact_outside_window"})))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "group_binding_friend_contact_outside_window") ||
		strings.Contains(w.Body.String(), "private nickname") {
		t.Fatal("out-of-window diagnosis leaked upstream details or lost its reason")
	}
	for _, prefix := range []string{"native_", "lajiaoliao_"} {
		for suffix, expected := range map[string]string{
			"not_found":             "group_binding_friend_contact_not_found",
			"outside_window":        "group_binding_friend_contact_outside_window",
			"processed":             "group_binding_friend_contact_processed",
			"ambiguous":             "group_binding_friend_contact_ambiguous",
			"timestamp_unavailable": "group_binding_friend_contact_time_unavailable",
			"timestamp_invalid":     "group_binding_friend_contact_time_unavailable",
			"changed":               "group_binding_friend_application_changed",
			"expired":               "group_binding_challenge_expired",
		} {
			code := prefix + "onboarding_contact_" + suffix
			var upstream error = &nativeauth.Error{Code: code}
			if prefix == "lajiaoliao_" {
				upstream = &lajiaoliao.Failure{Code: code}
			}
			w := httptest.NewRecorder()
			sendError(w, bindingFriendReadError(fmt.Errorf("synthetic private nickname and note: %w", upstream)))
			var response struct {
				Msg string `json:"msg"`
			}
			if w.Code != 409 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Msg != expected ||
				strings.Contains(w.Body.String(), "private nickname") {
				t.Fatal("contact read classification leaked or lost its safe reason", code)
			}
		}
	}
	w = httptest.NewRecorder()
	sendError(w, bindingFriendReadError(&lajiaoliao.Failure{Code: "lajiaoliao_onboarding_contact_incomplete"}))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "group_binding_friend_contact_incomplete") {
		t.Fatal("incomplete sender-only approval window was not explained")
	}
}

func TestBindingContactDirectoryFailureNeverExposesUpstreamContent(t *testing.T) {
	for reason, expected := range map[string]string{
		"invalid":   "group_binding_friend_response_invalid",
		"no_data":   "group_binding_friend_directory_no_data",
		"missing":   "group_binding_friend_directory_missing",
		"count":     "group_binding_friend_directory_count",
		"recipient": "group_binding_friend_directory_recipient",
		"rows":      "group_binding_friend_directory_record",
		"state":     "group_binding_friend_directory_record",
		"identity":  "group_binding_friend_directory_record",
	} {
		t.Run(reason, func(t *testing.T) {
			w := httptest.NewRecorder()
			upstream := fmt.Errorf("synthetic private applicant and note: %w",
				&lajiaoliao.Failure{Code: "lajiaoliao_onboarding_contact_directory_" + reason})
			sendError(w, bindingFriendReadError(upstream))
			var response struct {
				Msg string `json:"msg"`
			}
			if w.Code != 502 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Msg != expected ||
				strings.Contains(w.Body.String(), "private applicant") {
				t.Fatal("directory failure must expose only its fixed category")
			}
		})
	}
}
