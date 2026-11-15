package lajiaoliao

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"wangshangliao/internal/nativeauth"
)

func onboardingFriendRow(sender, recipient string, created int64, note string) map[string]any {
	return map[string]any{"fromUserID": sender, "toUserID": recipient, "createTime": created,
		"fromNickname": "synthetic", "reqMsg": note}
}

func onboardingFriendRaw(t *testing.T, rows ...map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"friendRequests": rows})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestOnboardingFriendChiliExactNoteStringIDsAndNoPublicNote(t *testing.T) {
	const code = "proof-synthetic-chili"
	session := Session{UserID: "self-string", IMToken: "synthetic", DeviceID: "device-fixture"}
	calls := 0
	client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
		calls++
		if r.URL.Path != "/friend/get_friend_apply_list" || len(body) != 2 || body["userID"] != session.UserID ||
			!reflect.DeepEqual(body["pagination"], map[string]any{"pageNumber": float64(1), "showNumber": float64(100)}) {
			t.Fatal("proof request escaped bounded first page")
		}
		// Use the vendored SDK contract, including omitted proto3 pending zero.
		return map[string]any{"friendRequests": []*sdkws.FriendRequest{{
			FromUserID: "customer-string", ToUserID: session.UserID, CreateTime: 1700000000, ReqMsg: code,
		}}}, nil
	})
	proof, err := client.OnboardingFriend(context.Background(), session, code)
	if err != nil || proof.ApplicationID != "1700000000" || proof.SenderID != "customer-string" ||
		proof.RecipientID != session.UserID || proof.State != "pending" || calls != 1 {
		t.Fatal("exact string-scoped proof rejected", err)
	}
	ordinary, _, err := client.FriendRequests(context.Background(), session, 1)
	if err != nil || len(ordinary) != 1 {
		t.Fatal("normal friend request projection changed", err)
	}
	for _, value := range []any{proof, ordinary} {
		raw, _ := json.Marshal(value)
		if strings.Contains(string(raw), code) || strings.Contains(string(raw), "reqMsg") {
			t.Fatal("note escaped private adapter")
		}
	}
	if calls != 2 {
		t.Fatal("proof read paginated or retried")
	}
}

func TestOnboardingFriendChiliRejectsAmbiguousRecipientAndNote(t *testing.T) {
	const code = "proof-synthetic"
	cases := []struct {
		name string
		edit func(map[string]any) []map[string]any
	}{
		{"other_recipient", func(row map[string]any) []map[string]any {
			row["toUserID"] = "other"
			return []map[string]any{row}
		}},
		{"self_sender", func(row map[string]any) []map[string]any {
			row["fromUserID"] = "self"
			return []map[string]any{row}
		}},
		{"approved", func(row map[string]any) []map[string]any {
			row["handleResult"] = 1
			return []map[string]any{row}
		}},
		{"unknown_state", func(row map[string]any) []map[string]any {
			row["handleResult"] = 2
			return []map[string]any{row}
		}},
		{"inexact_note", func(row map[string]any) []map[string]any {
			row["reqMsg"] = code + " "
			return []map[string]any{row}
		}},
		{"missing_note", func(row map[string]any) []map[string]any {
			delete(row, "reqMsg")
			return []map[string]any{row}
		}},
		{"wrong_note_shape", func(row map[string]any) []map[string]any {
			row["reqMsg"] = map[string]any{"content": code}
			return []map[string]any{row}
		}},
		{"two_senders_same_code", func(row map[string]any) []map[string]any {
			return []map[string]any{row, onboardingFriendRow("another", "self", 1700000001, code)}
		}},
		{"same_sender_two_pending", func(row map[string]any) []map[string]any {
			return []map[string]any{row, onboardingFriendRow("customer", "self", 1700000001, "unrelated")}
		}},
		{"duplicate_row", func(row map[string]any) []map[string]any { return []map[string]any{row, row} }},
		{"over_limit", func(row map[string]any) []map[string]any { return make([]map[string]any, 101) }},
	}
	expected := nativeauth.OnboardingFriendEvidence{ApplicationID: "1700000000", SenderID: "customer", RecipientID: "self", State: "pending"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := onboardingFriendRaw(t, tc.edit(onboardingFriendRow("customer", "self", 1700000000, code))...)
			if _, err := parseOnboardingFriend(raw, "self", code, nil); err == nil {
				t.Fatal("unverified friend proof admitted")
			}
			if tc.name != "approved" {
				if _, err := parseOnboardingFriend(raw, "self", code, &expected); err == nil {
					t.Fatal("original-ID selector bypassed changed or ambiguous proof")
				}
			}
		})
	}
	for _, malformed := range []json.RawMessage{[]byte(`{}`), []byte(`{"friendRequests":null}`)} {
		if _, err := parseOnboardingFriend(malformed, "self", code, nil); err == nil {
			t.Fatal("unknown list treated as proof")
		}
	}
}

func TestOnboardingFriendChiliApprovalFreshFenceUnknownAndReadOnlyReconcile(t *testing.T) {
	for _, mode := range []string{"approved", "changed", "ambiguous", "revoked", "expired", "cancelled", "unknown", "duplicate_submit"} {
		t.Run(mode, func(t *testing.T) {
			session := Session{UserID: "self-string", IMToken: "synthetic", DeviceID: "device-fixture"}
			const code = "proof-synthetic"
			reads, writes, fences := 0, 0, 0
			rows := []map[string]any{onboardingFriendRow("customer", session.UserID, 1700000000, code)}
			client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
				switch r.URL.Path {
				case "/friend/get_friend_apply_list":
					reads++
					if !reflect.DeepEqual(body["pagination"], map[string]any{"pageNumber": float64(1), "showNumber": float64(100)}) {
						t.Fatal("approval reread paginated")
					}
					return map[string]any{"friendRequests": rows}, nil
				case "/friend/add_friend_response":
					writes++
					if fences != 1 || !reflect.DeepEqual(body, map[string]any{"fromUserID": "customer",
						"toUserID": session.UserID, "handleResult": float64(1), "handleMsg": ""}) {
						t.Fatal("approval escaped original sender/recipient or fence")
					}
					if mode == "unknown" {
						return nil, io.ErrUnexpectedEOF
					}
					return map[string]any{}, nil
				default:
					t.Fatal("unreviewed onboarding route")
					return nil, nil
				}
			})
			proof, err := client.OnboardingFriend(context.Background(), session, code)
			if err != nil || reads != 1 || writes != 0 {
				t.Fatal("preparation mutated or did not read once", err)
			}
			if mode == "changed" {
				rows[0]["reqMsg"] = "changed"
			}
			if mode == "ambiguous" {
				rows = append(rows, onboardingFriendRow("another", session.UserID, 1700000001, code))
			}
			deadline := time.Now().Add(time.Minute)
			if mode == "expired" {
				deadline = time.Now().Add(-time.Second)
			}
			err = client.ApproveOnboardingFriend(context.Background(), session, code, proof, deadline,
				func(work context.Context, current nativeauth.OnboardingFriendEvidence, send func(context.Context) error) error {
					fences++
					if current != proof || reads != 2 {
						t.Fatal("approval reused stale application")
					}
					if mode == "revoked" {
						return errors.New("synthetic-revoked")
					}
					if mode == "cancelled" {
						cancelled, cancel := context.WithCancel(work)
						cancel()
						return send(cancelled)
					}
					err := send(work)
					if mode == "duplicate_submit" {
						return send(work)
					}
					return err
				})
			if mode == "approved" {
				if err != nil || writes != 1 || fences != 1 {
					t.Fatal("single approval failed", err)
				}
				rows[0]["handleResult"] = 1
				result, err := client.ReconcileOnboardingFriend(context.Background(), session, code, proof)
				if err != nil || result.State != "approved" || writes != 1 || reads != 3 {
					t.Fatal("reconciliation repeated mutation", err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid or unknown approval reported success")
			}
			wantWrites := 0
			if mode == "unknown" || mode == "duplicate_submit" {
				wantWrites = 1
			}
			if writes != wantWrites {
				t.Fatal("unsafe submission or retry", writes)
			}
			if mode == "unknown" {
				var failure *Failure
				if !errors.As(err, &failure) || !failure.Submitted {
					t.Fatal("unknown submission lost status", err)
				}
			}
		})
	}
}
