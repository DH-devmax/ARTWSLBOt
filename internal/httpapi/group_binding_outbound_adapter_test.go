package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

func TestBindingOutboundReadErrorsAreFixedAndPrivate(t *testing.T) {
	for _, prefix := range []string{"native_", "lajiaoliao_"} {
		for suffix, expected := range map[string]struct {
			status int
			code   string
		}{
			"account_invalid": {400, "group_binding_outbound_account_invalid"},
			"not_found":       {404, "group_binding_outbound_target_not_found"},
			"ambiguous":       {409, "group_binding_outbound_target_ambiguous"},
			"self":            {409, "group_binding_outbound_self"},
			"shape":           {502, "group_binding_outbound_response_invalid"},
			"pending":         {409, "group_binding_outbound_pending"},
			"already_friend":  {409, "group_binding_outbound_already_friend"},
			"incoming":        {409, "group_binding_outbound_incoming_pending"},
			"policy":          {409, "group_binding_outbound_policy"},
			"blocked":         {409, "group_binding_outbound_blocked"},
			"expired":         {409, "group_binding_challenge_expired"},
			"unavailable":     {503, "group_binding_outbound_unavailable"},
		} {
			var upstream error
			code := prefix + "onboarding_outbound_" + suffix
			if prefix == "native_" {
				upstream = &nativeauth.Error{Code: code}
			} else {
				upstream = &lajiaoliao.Failure{Code: code}
			}
			err := bindingOutboundReadError(fmt.Errorf("synthetic sensitive target, token, note: %w", upstream))
			var failure *control.Failure
			if !errors.As(err, &failure) || failure.Status != expected.status || failure.Code != expected.code {
				t.Fatal("outbound provider diagnosis not fixed", code, err)
			}
			w := httptest.NewRecorder()
			sendError(w, err)
			if w.Code != expected.status || strings.Contains(w.Body.String(), "sensitive") ||
				strings.Contains(w.Body.String(), "token") || strings.Contains(w.Body.String(), code) {
				t.Fatal("provider text escaped fixed outbound error")
			}
		}
	}
	for _, upstream := range []error{gateway.ErrReadBusy, gateway.ErrReadOwner, gateway.ErrMemberScope,
		context.Canceled, context.DeadlineExceeded, errors.New("private arbitrary upstream text"),
		&nativeauth.Error{Code: "private-provider-code"}, &lajiaoliao.Failure{Code: "private-provider-code"}} {
		var failure *control.Failure
		if !errors.As(bindingOutboundReadError(upstream), &failure) ||
			strings.Contains(failure.Code, "private") {
			t.Fatal("unreviewed gateway or provider error escaped")
		}
	}
	original := control.Denied(403, "group_binding_pool_identity")
	if bindingOutboundReadError(original) != original || bindingOutboundReadError(nil) != nil {
		t.Fatal("control authorization or success changed")
	}
}

func TestBindingOutboundErrorsDistinguishTargetScopeAndIncompleteReads(t *testing.T) {
	for _, tc := range []struct {
		name     string
		upstream error
		status   int
		code     string
	}{
		{"target", &nativeauth.Error{Code: "native_onboarding_outbound_changed", Diagnostic: "synthetic private provider detail"}, 409, "group_binding_outbound_target_inconsistent"},
		{"incomplete", &nativeauth.Error{Code: "native_onboarding_outbound_read_incomplete", Diagnostic: "synthetic private provider detail"}, 409, "group_binding_outbound_read_incomplete"},
		{"identity", &nativeauth.Error{Code: "native_onboarding_outbound_identity"}, 409, "group_binding_outbound_scope_changed"},
		{"operation", &nativeauth.Error{Code: "native_onboarding_outbound_operation_invalid"}, 409, "group_binding_outbound_scope_changed"},
		{"chili_changed", &lajiaoliao.Failure{Code: "lajiaoliao_onboarding_outbound_changed"}, 409, "group_binding_outbound_scope_changed"},
		{"chili_identity", &lajiaoliao.Failure{Code: "lajiaoliao_onboarding_outbound_identity"}, 409, "group_binding_outbound_scope_changed"},
		{"chili_incomplete", &lajiaoliao.Failure{Code: "lajiaoliao_onboarding_outbound_incomplete"}, 409, "group_binding_outbound_read_incomplete"},
		{"scope", gateway.ErrMemberScope, 409, "group_binding_outbound_scope_changed"},
		{"cancelled", context.Canceled, 408, "group_binding_outbound_cancelled"},
		{"timeout", context.DeadlineExceeded, 504, "group_binding_outbound_timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, wrapped := range []bool{false, true} {
				upstream := tc.upstream
				if wrapped {
					upstream = fmt.Errorf("synthetic private provider detail: %w", upstream)
				}
				for _, adapt := range []func(error) error{bindingOutboundReadError, bindingOutboundSendError} {
					err := adapt(upstream)
					var failure *control.Failure
					if !errors.As(err, &failure) || failure.Status != tc.status || failure.Code != tc.code {
						t.Fatalf("wrong safe error, wrapped=%t: %v", wrapped, err)
					}
					w := httptest.NewRecorder()
					sendError(w, err)
					var body struct {
						Code int    `json:"code"`
						Msg  string `json:"msg"`
					}
					if w.Code != tc.status || json.Unmarshal(w.Body.Bytes(), &body) != nil ||
						body.Code != tc.status || body.Msg != tc.code ||
						strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "provider") {
						t.Fatal("safe error response changed classification or exposed provider details")
					}
				}
			}
		})
	}
}

func TestBindingOutboundSendErrorPreservesUnknownSubmittedOutcome(t *testing.T) {
	for _, code := range []string{"native_transport_unknown", "native_response_invalid", "native_business_rejected"} {
		var result *nativeauth.Error
		err := bindingOutboundSendError(fmt.Errorf("synthetic private target: %w", &nativeauth.Error{Code: code, Submitted: true}))
		if !errors.As(err, &result) || !result.Submitted || result.Code != code ||
			strings.Contains(err.Error(), "private") {
			t.Fatal("submitted native outcome lost durable recovery semantics")
		}
	}
	for _, code := range []string{"lajiaoliao_result_unknown", "lajiaoliao_response_invalid", "lajiaoliao_rejected"} {
		var result *lajiaoliao.Failure
		err := bindingOutboundSendError(fmt.Errorf("synthetic private target: %w", &lajiaoliao.Failure{Code: code, Submitted: true}))
		if !errors.As(err, &result) || !result.Submitted || result.Code != code ||
			strings.Contains(err.Error(), "private") {
			t.Fatal("submitted Chili outcome lost durable recovery semantics")
		}
	}
	var failure *control.Failure
	if !errors.As(bindingOutboundSendError(&nativeauth.Error{Code: "unreviewed-private-code", Submitted: true}), &failure) ||
		failure.Code != "group_binding_outbound_unavailable" || bindingOutboundSendError(nil) != nil {
		t.Fatal("unreviewed send error escaped the fixed diagnosis")
	}
}

func TestBindingOutboundSendErrorKeepsOnlyBoundedProviderNumber(t *testing.T) {
	for _, number := range []int{-1, 0, 1001, 429, 9999, 10000} {
		source := &nativeauth.Error{Code: "native_business_rejected", Number: number,
			Submitted: true, Diagnostic: "synthetic-private-provider-body"}
		err := bindingOutboundSendError(fmt.Errorf("synthetic-private-token: %w", source))
		var retained *nativeauth.Error
		expected := number
		if expected < 1 || expected > 9999 {
			expected = 0
		}
		if !errors.As(err, &retained) || retained.Number != expected || retained.Diagnostic != "" ||
			!retained.Submitted || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("submission diagnosis leaked text or lost its numeric evidence")
		}
	}
}

func TestBindingOutboundRequestMapsScopeAndRejectsTrustedBrowserFields(t *testing.T) {
	req := control.GroupBindingOutboundRequest{
		GroupBindingNativeScope: control.GroupBindingNativeScope{Platform: "lajiaoliao", AccountID: captchaTestTarget,
			AccountEpoch: 7, AccountVersion: 8},
		OperationID: captchaTestOther, SelfID: "robot-string", ExpiresAt: time.Now().UTC().Add(time.Minute),
		Target: control.GroupBindingFriendTarget{NativeID: "customer-string", AddAccount: "search-string", Name: "synthetic"},
	}
	got := bindingOutboundRequest(req)
	if got.Platform != req.Platform || got.AccountID != req.AccountID || got.AccountEpoch != req.AccountEpoch ||
		got.AccountVersion != req.AccountVersion || got.OperationID != req.OperationID ||
		got.SelfID != req.SelfID || !got.ExpiresAt.Equal(req.ExpiresAt) ||
		got.Target.NativeID != req.Target.NativeID || got.Target.AddAccount != req.Target.AddAccount ||
		got.Target.Name != req.Target.Name || got.Target.AlreadyFriend != req.Target.AlreadyFriend {
		t.Fatal("adapter changed trusted scope or original request")
	}
	for _, payload := range []string{
		`{"version":1,"native_id":"forged"}`, `{"version":1,"account_id":"forged"}`,
		`{"version":1,"operation_id":"forged"}`, `{"version":1,"target":{"native_id":"forged"}}`,
		`{"version":1,"self_id":"forged"}`, `{"version":1,"expires_at":"2099-01-01T00:00:00Z"}`,
	} {
		r := httptest.NewRequest("POST", "/synthetic/friend-send", strings.NewReader(payload))
		r.Header.Set("Content-Type", "application/json")
		var input control.GroupBindingConfirmation
		if err := decode(httptest.NewRecorder(), r, &input); err == nil {
			t.Fatal("browser supplied trusted send scope")
		}
	}
	var in control.GroupBindingFriendTargetInput
	if json.Unmarshal([]byte(`{"version":1,"account":"search-string"}`), &in) != nil ||
		in.Version != 1 || in.Account != "search-string" || in.Key != "" {
		t.Fatal("browser target input contract changed")
	}
}
