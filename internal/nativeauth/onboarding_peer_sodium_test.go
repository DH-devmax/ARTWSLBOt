//go:build sodium && cgo

package nativeauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestOnboardingAuthenticatedPeerKeepsPolicyAndSingleSubmission(t *testing.T) {
	for _, mode := range []string{"pending", "default_account", "default_account_string", "default_account_friends",
		"default_account_revoked", "default_account_unknown", "already_friends", "old_pending",
		"incoming", "blocked", "policy", "wrong_uid", "wrong_account", "changed_account",
		"revoked", "cancelled", "duplicate_submit", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			raw, err := NewClient(syntheticDeployment())
			if err != nil {
				t.Fatal(err)
			}
			c := raw.(*Client)
			defer c.Close()
			c.jwt, c.fields[3] = "synthetic-peer-jwt", 7
			if err := c.RestrictOnboardingPeer("8", "340738649"); err != nil {
				t.Fatal(err)
			}
			var reads, writes, fences atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fields, _, params := desktopRequest(t, r, c.bodyKey)
				if fields[4] != 7 {
					t.Error("authenticated sender changed")
				}
				data := onboardingOutboundInfoRow()
				switch r.URL.Path {
				case onboardingOutboundInfoRoute:
					reads.Add(1)
					if len(params) != 2 || string(params["uid"]) != "8" || string(params["showTempChat"]) != "false" {
						t.Error("peer read escaped its fixed recipient")
					}
					switch mode {
					case "default_account", "default_account_revoked", "default_account_unknown":
						data["accountId"] = 0
					case "default_account_string":
						data["accountId"] = "0"
					case "default_account_friends":
						data["accountId"] = 0
						data["state"], data["state1"] = "FRIEND_STATE_GOOD", "FRIEND_STATE_GOOD"
					case "already_friends":
						data["state"], data["state1"] = "FRIEND_STATE_GOOD", "FRIEND_STATE_GOOD"
					case "old_pending":
						data["state"], data["state1"] = "FRIEND_STATE_APPLIED", "FRIEND_STATE_PENDING"
					case "incoming":
						data["state"], data["state1"] = "FRIEND_STATE_PENDING", "FRIEND_STATE_APPLIED"
					case "blocked":
						data["forbidAddFriend"] = true
					case "policy":
						data["friendPolicy"] = "FRIEND_ANSWER_CHECK"
					case "wrong_uid":
						data["uid"] = 9
					case "wrong_account":
						data["accountId"] = 340738650
					}
				case onboardingOutboundApplyRoute:
					writes.Add(1)
					note, _ := onboardingOutboundNote(onboardingOutboundTestOperation)
					var actual string
					_ = json.Unmarshal(params["applyMsg"], &actual)
					if fences.Load() != 1 || reads.Load() != 1 || actual != note ||
						string(params["friendId"]) != `{"id":340738649,"idType":"ID_TP_ACCOUNT"}` {
						t.Error("application changed its recipient or bypassed fresh policy/fence")
					}
					if mode == "unknown" || mode == "default_account_unknown" {
						w.WriteHeader(http.StatusBadGateway)
						return
					}
					data = map[string]any{"flId": "9007199254740993",
						"state": "FRIEND_STATE_APPLIED", "state1": "FRIEND_STATE_PENDING"}
				default:
					t.Error("known peer performed public search or an unexpected route", r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
			}))
			defer server.Close()
			c.config.Origin, c.http = server.URL, server.Client()
			request := OnboardingOutboundRequest{OperationID: onboardingOutboundTestOperation, SelfID: "7",
				Target: OnboardingOutboundTarget{ID: "8", Account: "340738649"}, ExpiresAt: time.Now().Add(time.Minute)}
			if mode == "changed_account" {
				request.Target.Account = "340738650"
			}
			revoked := errors.New("synthetic-peer-revoked")
			result, err := c.SendOnboardingOutboundFriend(context.Background(), request,
				func(work context.Context, target OnboardingOutboundTarget, submit func(context.Context) error) error {
					fences.Add(1)
					if target.ID != "8" || target.Account != "340738649" || !target.CanRequest {
						t.Error("peer evidence changed before final fence")
					}
					if mode == "revoked" || mode == "default_account_revoked" {
						return revoked
					}
					if mode == "cancelled" {
						stopped, cancel := context.WithCancel(work)
						cancel()
						return submit(stopped)
					}
					err := submit(work)
					if mode == "duplicate_submit" {
						return submit(work)
					}
					return err
				})
			wantWrites := int32(0)
			if mode == "pending" || mode == "default_account" || mode == "default_account_string" ||
				mode == "duplicate_submit" || mode == "unknown" || mode == "default_account_unknown" {
				wantWrites = 1
			}
			if writes.Load() != wantWrites {
				t.Fatal("peer application retried or escaped its fence", writes.Load(), err)
			}
			switch mode {
			case "pending", "default_account", "default_account_string":
				if err != nil || result.State != "pending" || result.RecipientID != "8" {
					t.Fatal("single peer request did not retain its receipt", result, err)
				}
			case "already_friends", "default_account_friends":
				if err != nil || result.State != "friends" || fences.Load() != 0 {
					t.Fatal("existing friend was reapplied", result, err)
				}
			default:
				if err == nil {
					t.Fatal("invalid or unknown peer request reported success")
				}
				if (mode == "revoked" || mode == "default_account_revoked") && !errors.Is(err, revoked) {
					t.Fatal("revocation lost its cause", err)
				}
				if mode == "unknown" || mode == "default_account_unknown" {
					var uncertain *Error
					if !errors.As(err, &uncertain) || !uncertain.Submitted {
						t.Fatal("unknown peer request became retryable", err)
					}
				}
			}
		})
	}
}

func TestOnboardingAuthenticatedPeerCannotBeRebound(t *testing.T) {
	for _, mode := range []string{"self", "invalid_uid", "invalid_account", "mobile", "closed", "uninitialized", "rebound"} {
		t.Run(mode, func(t *testing.T) {
			raw, err := NewClient(syntheticDeployment())
			if err != nil {
				t.Fatal(err)
			}
			c := raw.(*Client)
			defer c.Close()
			c.fields[3] = 7
			id, account := "8", "340738649"
			switch mode {
			case "self":
				id = "7"
			case "invalid_uid":
				id = "08"
			case "invalid_account":
				account = "not-an-account"
			case "mobile":
				c.mobile = true
			case "closed":
				c.Close()
			case "uninitialized":
				c.fields = nil
			case "rebound":
				if err := c.RestrictOnboardingPeer(id, account); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.RestrictOnboardingPeer(id, account); err == nil {
				t.Fatal("invalid or second peer mapping accepted")
			}
		})
	}
}

func TestOnboardingAuthenticatedPeerLookupRetainsIdentityReasonWithoutNetwork(t *testing.T) {
	for _, tc := range []struct {
		field, raw, reason string
	}{
		{"uid", "", "friend_info_uid_missing"},
		{"uid", "null", "friend_info_uid_null"},
		{"uid", "0", "friend_info_uid_zero"},
		{"uid", `"0"`, "friend_info_uid_zero"},
		{"uid", `"synthetic-private-identity"`, "friend_info_uid_invalid"},
		{"uid", "9", "friend_info_uid_mismatch"},
		{"accountId", "", "friend_info_account_missing"},
		{"accountId", "null", "friend_info_account_null"},
		{"accountId", "0", ""},
		{"accountId", `"0"`, ""},
		{"accountId", `"synthetic-private-identity"`, "friend_info_account_invalid"},
		{"accountId", "340738650", "friend_info_account_mismatch"},
		{"accountId", "340738649", ""},
	} {
		t.Run(tc.field+"/"+tc.raw, func(t *testing.T) {
			raw, err := NewClient(syntheticDeployment())
			if err != nil {
				t.Fatal(err)
			}
			c := raw.(*Client)
			defer c.Close()
			c.jwt, c.fields[3] = "synthetic-peer-jwt", 7
			if err := c.RestrictOnboardingPeer("8", "340738649"); err != nil {
				t.Fatal(err)
			}
			reads := 0
			c.http = &http.Client{Transport: settingsRoundTrip(func(r *http.Request) (*http.Response, error) {
				reads++
				_, _, params := desktopRequest(t, r, c.bodyKey)
				if r.URL.Path != onboardingOutboundInfoRoute || len(params) != 2 ||
					string(params["uid"]) != "8" || string(params["showTempChat"]) != "false" {
					t.Fatal("fixed peer lookup searched or submitted an application")
				}
				info := onboardingOutboundData(t, onboardingOutboundInfoRow())
				if tc.raw == "" {
					delete(info, tc.field)
				} else {
					info[tc.field] = json.RawMessage(tc.raw)
				}
				w := httptest.NewRecorder()
				if err := json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": info}); err != nil {
					t.Fatal(err)
				}
				return w.Result(), nil
			})}
			got, err := c.LookupOnboardingOutboundFriend(context.Background(), "340738649")
			if reads != 1 {
				t.Fatal("peer lookup retried or read another route", reads)
			}
			if tc.reason == "" {
				if err != nil || got.ID != "8" || got.Account != "340738649" || !got.CanRequest {
					t.Fatal("normal peer identity no longer passed", got, err)
				}
				return
			}
			var lookup *OnboardingLookupError
			var native *Error
			if got != (OnboardingOutboundTarget{}) || !errors.As(err, &lookup) ||
				lookup.Stage != "friend_info" || lookup.Reason != tc.reason ||
				!errors.As(err, &native) || native.Code != "native_onboarding_outbound_changed" || native.Submitted {
				t.Fatal("peer lookup lost its fixed field diagnosis", err)
			}
		})
	}
}
