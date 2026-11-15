//go:build sodium && cgo

package nativeauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wangshangliao/internal/nativewire"
)

func TestOnboardingOutboundWSLSignedSingleSubmissionAndFence(t *testing.T) {
	for _, mode := range []string{"pending", "already_friends", "old_pending", "old_pending_none", "incoming", "blocked", "policy",
		"wrong_account", "wrong_target", "wrong_self", "revoked", "expired", "cancelled", "cancelled_in_fence",
		"expired_in_fence", "no_submit", "duplicate_submit", "unknown", "wrong_direction_response",
		"oversized", "missing_state1", "nonce_failure", "fence_required", "server_rejected",
		"null_state", "null_state1", "null_policy", "null_forbidden", "null_apply_state"} {
		t.Run(mode, func(t *testing.T) {
			base, err := NewClient(syntheticDeployment())
			if err != nil {
				t.Fatal(err)
			}
			c := base.(*Client)
			defer c.Close()
			c.jwt, c.fields[3] = "synthetic-outbound-jwt", 7
			var reads, writes, fences, allocated atomic.Int32
			var nonceMu sync.Mutex
			nonces := map[uint64]bool{}
			c.requestIDs = func(ctx context.Context, now uint64) (uint64, error) {
				if mode == "nonce_failure" && allocated.Add(1) == 3 {
					return 0, errors.New("synthetic-nonce-unavailable")
				}
				return nextRequestID(&desktopIDs, now)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fields, _, params := desktopRequest(t, r, c.bodyKey)
				if fields[4] != 7 || r.Header.Get("x-id") != "7" {
					t.Error("request did not retain the authenticated sender")
				}
				nonceMu.Lock()
				if nonces[fields[6]] {
					t.Error("request nonce was reused")
				}
				nonces[fields[6]] = true
				nonceMu.Unlock()
				data := map[string]any{}
				switch r.URL.Path {
				case onboardingOutboundSearchRoute:
					reads.Add(1)
					if len(params) != 3 || string(params["key"]) != `"340738649"` ||
						string(params["qrcodeId"]) != `"0"` || string(params["type"]) != `"CONVERSATION_TYPE_SEARCH"` {
						t.Error("search departed from the pinned desktop contract")
					}
					data["items"] = []any{onboardingOutboundSearchRow()}
				case onboardingOutboundInfoRoute:
					reads.Add(1)
					if len(params) != 2 || string(params["uid"]) != "8" || string(params["showTempChat"]) != "false" {
						t.Error("friend policy read changed target or enabled temporary chat")
					}
					data = onboardingOutboundInfoRow()
					switch mode {
					case "already_friends":
						data["state"], data["state1"] = "FRIEND_STATE_GOOD", "FRIEND_STATE_GOOD"
					case "old_pending":
						data["state"], data["state1"] = "FRIEND_STATE_APPLIED", "FRIEND_STATE_PENDING"
					case "old_pending_none":
						data["state"], data["state1"] = "FRIEND_STATE_NONE", "FRIEND_STATE_PENDING"
					case "incoming":
						data["state"], data["state1"] = "FRIEND_STATE_PENDING", "FRIEND_STATE_APPLIED"
					case "blocked":
						data["forbidAddFriend"] = true
					case "policy":
						data["friendPolicy"] = "FRIEND_ANSWER_CHECK"
					case "wrong_account":
						data["accountId"] = 340738650
					case "wrong_target":
						data["uid"] = 9
					case "null_state":
						data["state"] = nil
					case "null_state1":
						data["state1"] = nil
					case "null_policy":
						data["friendPolicy"] = nil
					case "null_forbidden":
						data["forbidAddFriend"] = nil
					}
				case onboardingOutboundApplyRoute:
					writes.Add(1)
					note, _ := onboardingOutboundNote(onboardingOutboundTestOperation)
					var actualNote string
					_ = json.Unmarshal(params["applyMsg"], &actualNote)
					if fences.Load() != 1 || len(params) != 7 || actualNote != note ||
						string(params["friendId"]) != `{"id":340738649,"idType":"ID_TP_ACCOUNT"}` ||
						string(params["addType"]) != `"FRIEND_SRC_MEMBER"` || string(params["nickName"]) != `""` ||
						string(params["classId"]) != "0" ||
						string(params["friendCircle"]) != `{"notSeeHim":true,"notSeeMe":true}` ||
						string(params["friendPolicy"]) != `"FRIEND_NEED_APPROVAL"` {
						t.Error("unfenced or wrong-target friend application")
					}
					if mode == "unknown" {
						w.WriteHeader(http.StatusBadGateway)
						return
					}
					if mode == "oversized" {
						w.Write(bytes.Repeat([]byte("x"), nativewire.Limit+4097))
						return
					}
					if mode == "server_rejected" {
						w.Write([]byte(`{"code":1001,"msg":"synthetic-rejection","data":{}}`))
						return
					}
					data = map[string]any{
						"flId": "9007199254740993", "state": "FRIEND_STATE_APPLIED", "state1": "FRIEND_STATE_PENDING",
					}
					if mode == "wrong_direction_response" {
						data["state"], data["state1"] = "FRIEND_STATE_PENDING", "FRIEND_STATE_APPLIED"
					}
					if mode == "missing_state1" {
						delete(data, "state1")
					}
					if mode == "null_apply_state" {
						data["state"] = nil
					}
				default:
					t.Error("unreviewed outbound route", r.URL.Path)
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
			}))
			defer server.Close()
			c.config.Origin, c.http = server.URL, server.Client()
			request := OnboardingOutboundRequest{
				OperationID: onboardingOutboundTestOperation, SelfID: "7",
				Target: OnboardingOutboundTarget{ID: "8", Account: "340738649"}, ExpiresAt: time.Now().Add(time.Minute),
			}
			if mode == "wrong_self" {
				request.SelfID = "9"
			}
			if mode == "expired" {
				request.ExpiresAt = time.Now().Add(-time.Second)
			}
			if mode == "expired_in_fence" {
				request.ExpiresAt = time.Now().Add(time.Second)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			revoked := errors.New("synthetic-authority-revoked")
			fence := OnboardingOutboundFence(func(work context.Context, target OnboardingOutboundTarget, submit func(context.Context) error) error {
				fences.Add(1)
				if target.ID != "8" || target.Account != "340738649" || !target.CanRequest || reads.Load() != 2 {
					t.Error("final fence did not receive fresh target and policy evidence")
				}
				switch mode {
				case "revoked":
					return revoked
				case "no_submit":
					return nil
				case "cancelled_in_fence":
					stopped, stop := context.WithCancel(work)
					stop()
					return submit(stopped)
				case "expired_in_fence":
					timer := time.NewTimer(time.Until(request.ExpiresAt) + time.Millisecond)
					<-timer.C
				}
				err := submit(work)
				if mode == "duplicate_submit" {
					return submit(work)
				}
				return err
			})
			if mode == "fence_required" {
				fence = nil
			}
			got, err := c.SendOnboardingOutboundFriend(ctx, request, fence)
			wantWrites := int32(0)
			switch mode {
			case "pending", "duplicate_submit", "unknown", "wrong_direction_response", "oversized",
				"missing_state1", "server_rejected", "null_apply_state":
				wantWrites = 1
			}
			if writes.Load() != wantWrites {
				t.Fatal("friend request was retried or submitted outside its fence", writes.Load(), err)
			}
			switch mode {
			case "pending":
				if err != nil || got.State != "pending" || got.IsFriend || got.SenderID != "7" ||
					got.RecipientID != "8" || got.ApplicationID != "9007199254740993" ||
					got.OperationID != request.OperationID {
					t.Fatal("single outbound receipt lost scope or operation", got, err)
				}
			case "already_friends":
				if err != nil || got.State != "friends" || !got.IsFriend || fences.Load() != 0 || reads.Load() != 2 {
					t.Fatal("existing friendship caused another request", got, err)
				}
			default:
				if err == nil {
					t.Fatal("blocked or unknown submission reported success", got)
				}
				if mode == "revoked" && !errors.Is(err, revoked) {
					t.Fatal("pre-submit fence rejection was obscured", err)
				}
				if mode == "old_pending_none" {
					var native *Error
					if !errors.As(err, &native) || native.Code != "native_onboarding_outbound_pending" {
						t.Fatal("outbound pending relationship was misclassified", err)
					}
				}
				if wantWrites != 0 {
					var native *Error
					if !errors.As(err, &native) || !native.Submitted {
						t.Fatal("uncertain or rejected submission lost no-retry evidence", err)
					}
				}
			}
		})
	}
}

func TestOnboardingOutboundWSLLookupAndReconcileAreReadOnly(t *testing.T) {
	for _, mode := range []string{"exact", "unrelated_pending", "inbound_same_note", "wrong_target_note",
		"approved_not_friend", "friends_now", "missing", "changed_target", "wrong_self",
		"null_direction", "null_application_state"} {
		t.Run(mode, func(t *testing.T) {
			base, err := NewClient(syntheticDeployment())
			if err != nil {
				t.Fatal(err)
			}
			c := base.(*Client)
			defer c.Close()
			c.jwt, c.fields[3] = "synthetic-outbound-reconcile", 7
			var reads, writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _, params := desktopRequest(t, r, c.bodyKey)
				data := map[string]any{}
				switch r.URL.Path {
				case onboardingOutboundSearchRoute:
					reads.Add(1)
					data["items"] = []any{onboardingOutboundSearchRow()}
				case onboardingOutboundInfoRoute:
					reads.Add(1)
					data = onboardingOutboundInfoRow()
					data["state"], data["state1"] = "FRIEND_STATE_APPLIED", "FRIEND_STATE_PENDING"
					if mode == "friends_now" {
						data["state"], data["state1"] = "FRIEND_STATE_GOOD", "FRIEND_STATE_GOOD"
					}
					if mode == "changed_target" {
						data["uid"] = 9
					}
				case "/v1/friend/friend-apply-list":
					reads.Add(1)
					if len(params) != 1 || string(params["lastId"]) != `"0"` {
						t.Error("reconcile invented pagination or approval")
					}
					note, _ := onboardingOutboundNote(onboardingOutboundTestOperation)
					row := onboardingFriendRow("9007199254740993", "8", note)
					row["direction"] = "FROM_ME"
					switch mode {
					case "unrelated_pending":
						row["applyMsg"] = "an older request"
					case "inbound_same_note":
						row["direction"] = "FROM_HIM"
					case "wrong_target_note":
						row["user"] = map[string]any{"uid": 9}
					case "approved_not_friend":
						row["state"] = "FRIENDLOG_STATE_AGREE"
					case "null_direction":
						row["direction"] = nil
					case "null_application_state":
						row["state"] = nil
					}
					data["list"] = []any{row}
					if mode == "missing" {
						data["list"] = []any{}
					}
				default:
					writes.Add(1)
					t.Error("read-only onboarding submitted an effect", r.URL.Path)
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
			}))
			defer server.Close()
			c.config.Origin, c.http = server.URL, server.Client()
			target, err := c.LookupOnboardingOutboundFriend(context.Background(), "340738649")
			if mode == "changed_target" {
				if err == nil {
					t.Fatal("lookup admitted mismatched profile identity")
				}
				return
			}
			if err != nil || reads.Load() != 2 || writes.Load() != 0 {
				t.Fatal("lookup performed an effect or unbounded requests", target, err)
			}
			request := OnboardingOutboundRequest{
				OperationID: onboardingOutboundTestOperation, SelfID: "7", Target: target,
				ExpiresAt: time.Now().Add(-time.Minute),
			}
			if mode == "wrong_self" {
				request.SelfID = "9"
			}
			got, err := c.ReconcileOnboardingOutboundFriend(context.Background(), request)
			if mode == "null_direction" || mode == "null_application_state" {
				if err == nil || reads.Load() != 5 || writes.Load() != 0 {
					t.Fatal("null application evidence resolved or replayed the operation", got, err)
				}
				return
			}
			if mode == "wrong_self" {
				if err == nil || reads.Load() != 2 {
					t.Fatal("wrong authenticated sender caused a reconcile read", got, err)
				}
				return
			}
			wantState, wantReads := "unknown", int32(5)
			switch mode {
			case "exact":
				wantState = "pending"
			case "approved_not_friend":
				wantState = "approved"
			case "friends_now":
				wantState, wantReads = "friends", 4
			}
			if err != nil || got.State != wantState || reads.Load() != wantReads || writes.Load() != 0 ||
				got.IsFriend != (mode == "friends_now") || got.OperationID != request.OperationID {
				t.Fatal("reconciliation claimed another operation or resent", got, err, reads.Load(), writes.Load())
			}
		})
	}
}

func TestOnboardingOutboundWSLRawMutationStillRequiresFinalFence(t *testing.T) {
	base, err := NewClient(syntheticDeployment())
	if err != nil {
		t.Fatal(err)
	}
	c := base.(*Client)
	defer c.Close()
	c.jwt, c.fields[3] = "synthetic-outbound-raw", 7
	var native *Error
	if _, err := c.Request(context.Background(), onboardingOutboundApplyRoute, map[string]any{}); !errors.As(err, &native) ||
		native.Code != "native_setting_fence_required" || native.Submitted {
		t.Fatal("raw outbound request bypassed the mutation fence", err)
	}
}

func TestOnboardingOutboundWSLReadResponseLimitsDoNotRetry(t *testing.T) {
	for _, mode := range []string{"oversized_search", "oversized_info", "search_rate_limit", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			base, err := NewClient(syntheticDeployment())
			if err != nil {
				t.Fatal(err)
			}
			c := base.(*Client)
			defer c.Close()
			c.jwt, c.fields[3] = "synthetic-outbound-read-bound", 7
			var reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads.Add(1)
				desktopRequest(t, r, c.bodyKey)
				if r.URL.Path != onboardingOutboundSearchRoute && r.URL.Path != onboardingOutboundInfoRoute {
					t.Error("read limit test escaped fixed read routes")
				}
				if mode == "search_rate_limit" {
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				if r.URL.Path == onboardingOutboundSearchRoute && mode == "oversized_info" {
					json.NewEncoder(w).Encode(map[string]any{"code": 0,
						"data": map[string]any{"items": []any{onboardingOutboundSearchRow()}}})
					return
				}
				w.Write(bytes.Repeat([]byte("x"), nativewire.Limit+4097))
			}))
			defer server.Close()
			c.config.Origin, c.http = server.URL, server.Client()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			if _, err := c.LookupOnboardingOutboundFriend(ctx, "340738649"); err == nil {
				t.Fatal("unbounded or cancelled lookup succeeded")
			}
			want := int32(1)
			if mode == "oversized_info" {
				want = 2
			}
			if mode == "cancelled" {
				want = 0
			}
			if reads.Load() != want {
				t.Fatal("lookup retried a failed response", reads.Load(), want)
			}
			if mode == "search_rate_limit" {
				if _, err := c.LookupOnboardingOutboundFriend(ctx, "340738649"); err == nil || reads.Load() != 1 {
					t.Fatal("429 cooldown was bypassed or retried", err, reads.Load())
				}
			}
		})
	}
}
