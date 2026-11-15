package lajiaoliao

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

const outboundOperation = "12345678-9abc-4def-8123-123456789abc"

var outboundSession = Session{UserID: "robot-string", IMToken: "im-synthetic",
	ChatToken: "chat-synthetic", DeviceID: "device-fixture"}

func outboundSearchUser() map[string]any {
	return map[string]any{"userID": "target-string", "account": "native-account",
		"nickname": "synthetic", "phoneNumber": "not-returned"}
}

func outboundSearchResponse(user map[string]any) map[string]any {
	return map[string]any{"total": 1, "users": []any{user}}
}

func outboundFriendResponse(friend bool) map[string]any {
	rows := []any{}
	if friend {
		rows = append(rows, map[string]any{"ownerUserID": outboundSession.UserID,
			"friendUser": map[string]any{"userID": "target-string", "nickname": "synthetic"}})
	}
	return map[string]any{"friendsInfo": rows}
}

func outboundApplication(note string, created int64) map[string]any {
	return map[string]any{"fromUserID": outboundSession.UserID, "toUserID": "target-string",
		"createTime": created, "reqMsg": note}
}

func TestOnboardingOutboundChiliExactSearchContractAndStringIdentity(t *testing.T) {
	for _, friend := range []bool{false, true} {
		calls := 0
		client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
			calls++
			switch r.URL.Path {
			case "/user/search/full":
				if r.Header.Get("token") != outboundSession.ChatToken || r.Header.Get("platform") != "3" ||
					!reflect.DeepEqual(body, map[string]any{"keyword": "native-account", "normal": float64(0),
						"pagination": map[string]any{"pageNumber": float64(1), "showNumber": float64(1)}}) {
					t.Fatal("search escaped single account or Chat authentication")
				}
				return outboundSearchResponse(outboundSearchUser()), nil
			case "/friend/get_designated_friends":
				if r.Header.Get("token") != outboundSession.IMToken || r.Header.Get("platform") != "5" ||
					!reflect.DeepEqual(body, map[string]any{"ownerUserID": outboundSession.UserID,
						"friendUserIDs": []any{"target-string"}}) {
					t.Fatal("friend read escaped selected target")
				}
				return outboundFriendResponse(friend), nil
			default:
				t.Fatalf("unexpected route %s", r.URL.Path)
				return nil, nil
			}
		})
		target, err := client.LookupOnboardingOutboundFriend(context.Background(), outboundSession, "native-account")
		if err != nil || calls != 2 || target.ID != "target-string" || target.Name != "synthetic" ||
			target.Account != "native-account" || target.IsFriend != friend || target.CanRequest == friend {
			t.Fatal("wrong bounded target projection", target, err)
		}
		raw, _ := json.Marshal(target)
		if strings.Contains(string(raw), "not-returned") || strings.Contains(string(raw), "phoneNumber") {
			t.Fatal("private search data escaped projection")
		}
	}
}

func TestOnboardingOutboundChiliRejectsFuzzyAmbiguousAndInvalidSearch(t *testing.T) {
	tests := []struct {
		name string
		edit func(map[string]any, map[string]any)
	}{
		{"fuzzy_nickname_or_phone", func(_ map[string]any, u map[string]any) { u["account"] = "other-account" }},
		{"ambiguous", func(r map[string]any, _ map[string]any) { r["total"] = 2 }},
		{"self", func(_ map[string]any, u map[string]any) { u["userID"] = outboundSession.UserID }},
		{"banned", func(_ map[string]any, u map[string]any) { u["isBanned"] = true }},
		{"disabled", func(_ map[string]any, u map[string]any) { u["accountStatus"] = 1 }},
		{"deleted", func(_ map[string]any, u map[string]any) { u["accountStatus"] = "2" }},
		{"invalid_status", func(_ map[string]any, u map[string]any) { u["accountStatus"] = map[string]int{"x": 0} }},
		{"missing_id", func(_ map[string]any, u map[string]any) { delete(u, "userID") }},
		{"numeric_id", func(_ map[string]any, u map[string]any) { u["userID"] = 1234 }},
		{"bad_name", func(_ map[string]any, u map[string]any) { u["nickname"] = "bad\x00name" }},
		{"missing_total", func(r map[string]any, _ map[string]any) { delete(r, "total") }},
		{"missing_list", func(r map[string]any, _ map[string]any) { delete(r, "users") }},
		{"not_found", func(r map[string]any, _ map[string]any) { r["total"], r["users"] = 0, []any{} }},
		{"over_window", func(r map[string]any, u map[string]any) { r["total"], r["users"] = 2, []any{u, u} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			user := outboundSearchUser()
			result := outboundSearchResponse(user)
			tc.edit(result, user)
			raw, _ := json.Marshal(result)
			if _, err := parseOnboardingOutboundSearch(raw, outboundSession.UserID, "native-account"); err == nil {
				t.Fatal("unverified search admitted")
			}
		})
	}
	for _, account := range []string{"", "has space", "leading ", "bad\taccount", strings.Repeat("a", 129), "\xff"} {
		calls := 0
		client := nativeFixture(t, func(*http.Request, map[string]any) (any, error) { calls++; return nil, nil })
		if _, err := client.LookupOnboardingOutboundFriend(context.Background(), outboundSession, account); err == nil || calls != 0 {
			t.Fatal("invalid account reached network")
		}
	}
	user := outboundSearchUser()
	user["account"], user["userID"], user["accountStatus"] = "", "native-account", "0"
	raw, _ := json.Marshal(outboundSearchResponse(user))
	if target, err := parseOnboardingOutboundSearch(raw, outboundSession.UserID, "native-account"); err != nil || target.ID != "native-account" {
		t.Fatal("exact native string ID rejected", err)
	}
}

func TestOnboardingOutboundChiliBoundedSendFencesAndNoRetry(t *testing.T) {
	for _, mode := range []string{"success", "denied", "cancelled", "parent_cancelled", "expired", "nil_fence",
		"no_submit", "unknown", "duplicate", "changed", "already_friend", "pending", "historical_same_operation", "incomplete"} {
		t.Run(mode, func(t *testing.T) {
			reads, writes, fences := 0, 0, 0
			note, err := onboardingOutboundNote(outboundOperation)
			if err != nil || utf8.RuneCountInString(note) != 30 ||
				note != "\u63a5\u5165123456789abc4def8123\n[src:1]" {
				t.Fatal("unstable or oversized operation note")
			}
			client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
				if r.URL.Path != "/friend/add_friend" {
					reads++
				}
				switch r.URL.Path {
				case "/user/search/full":
					user := outboundSearchUser()
					if mode == "changed" {
						user["userID"] = "replacement-string"
					}
					return outboundSearchResponse(user), nil
				case "/friend/get_designated_friends":
					return outboundFriendResponse(mode == "already_friend"), nil
				case "/friend/get_self_friend_apply_list":
					if !reflect.DeepEqual(body, map[string]any{"userID": outboundSession.UserID,
						"pagination": map[string]any{"pageNumber": float64(1), "showNumber": float64(100)}}) {
						t.Fatal("application read expanded")
					}
					rows := []any{}
					if mode == "pending" {
						rows = append(rows, outboundApplication("another operation", 1700000000))
					}
					if mode == "historical_same_operation" {
						row := outboundApplication(note, 1700000000)
						row["handleResult"] = 1
						rows = append(rows, row)
					}
					total := len(rows)
					if mode == "incomplete" {
						total = 101
					}
					return map[string]any{"friendRequests": rows, "total": total}, nil
				case "/friend/add_friend":
					if fences != 1 || reads != 3 || r.Header.Get("token") != outboundSession.IMToken ||
						r.Header.Get("platform") != "5" || !reflect.DeepEqual(body, map[string]any{
						"fromUserID": outboundSession.UserID, "toUserID": "target-string", "reqMsg": note, "ex": ""}) {
						t.Fatal("send escaped confirmed target, original operation or fence")
					}
					writes++
					if mode == "unknown" {
						return nil, io.ErrUnexpectedEOF
					}
					return map[string]any{}, nil
				default:
					t.Fatal("unexpected route", r.URL.Path)
					return nil, nil
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			deadline := time.Now().Add(time.Minute)
			if mode == "expired" {
				deadline = time.Now().Add(-time.Second)
			}
			fence := OnboardingOutboundFence(func(work context.Context, target OnboardingOutboundTarget, submit func(context.Context) error) error {
				fences++
				if target.ID != "target-string" || !target.CanRequest || target.IsFriend {
					t.Fatal("fence did not receive fresh target")
				}
				switch mode {
				case "denied":
					return errors.New("synthetic revoked")
				case "no_submit":
					return nil
				case "cancelled":
					final, stop := context.WithCancel(work)
					stop()
					return submit(final)
				case "parent_cancelled":
					cancel()
					return submit(context.Background())
				default:
					err := submit(work)
					if mode == "duplicate" {
						return submit(work)
					}
					return err
				}
			})
			if mode == "nil_fence" {
				fence = nil
			}
			result, err := client.SendOnboardingOutboundFriend(ctx, outboundSession, OnboardingOutboundRequest{
				OperationID: outboundOperation, SelfID: outboundSession.UserID, ExpiresAt: deadline,
				Target: OnboardingOutboundTarget{ID: "target-string", Account: "native-account", Name: "old nickname"},
			}, fence)
			switch mode {
			case "success":
				if err != nil || writes != 1 || result.State != "pending" || result.IsFriend || result.ApplicationID != "" {
					t.Fatal("submission receipt confused with accepted friendship", result, err)
				}
			case "already_friend":
				if err != nil || writes != 0 || fences != 0 || result.State != "friends" || !result.IsFriend {
					t.Fatal("existing friendship caused another request", result, err)
				}
			case "unknown", "duplicate":
				var failure *Failure
				if !errors.As(err, &failure) || !failure.Submitted || writes != 1 {
					t.Fatal("unknown or duplicate send replayed", err, writes)
				}
			default:
				if err == nil || writes != 0 {
					t.Fatal("invalid fenced submission admitted", err, writes)
				}
			}
		})
	}
}

func TestOnboardingOutboundChiliConcurrentFenceCannotSendTwice(t *testing.T) {
	var writes atomic.Int32
	client := nativeFixture(t, func(r *http.Request, _ map[string]any) (any, error) {
		switch r.URL.Path {
		case "/user/search/full":
			return outboundSearchResponse(outboundSearchUser()), nil
		case "/friend/get_designated_friends":
			return outboundFriendResponse(false), nil
		case "/friend/get_self_friend_apply_list":
			return map[string]any{"friendRequests": []any{}, "total": 0}, nil
		case "/friend/add_friend":
			writes.Add(1)
			return map[string]any{}, nil
		default:
			t.Fatal("unexpected route")
			return nil, nil
		}
	})
	_, err := client.SendOnboardingOutboundFriend(context.Background(), outboundSession,
		OnboardingOutboundRequest{OperationID: outboundOperation, SelfID: outboundSession.UserID,
			ExpiresAt: time.Now().Add(time.Minute), Target: OnboardingOutboundTarget{ID: "target-string", Account: "native-account"}},
		func(ctx context.Context, _ OnboardingOutboundTarget, submit func(context.Context) error) error {
			var wg sync.WaitGroup
			results := make(chan error, 2)
			for range 2 {
				wg.Go(func() { results <- submit(ctx) })
			}
			wg.Wait()
			close(results)
			success := 0
			for result := range results {
				if result == nil {
					success++
				}
			}
			if success != 1 {
				t.Fatal("concurrent submission callback admitted more than one send")
			}
			return nil
		})
	if err != nil || writes.Load() != 1 {
		t.Fatal("concurrent fence did not remain single use", err, writes.Load())
	}
}

func TestOnboardingOutboundChiliReconcileOnlyOriginalAndCurrentFriend(t *testing.T) {
	for _, mode := range []string{"pending", "approved", "rejected", "missing", "unrelated", "friends",
		"ambiguous", "reverse", "wrong_target", "duplicate", "incomplete", "missing_list", "wrong_state"} {
		t.Run(mode, func(t *testing.T) {
			reads := 0
			note, _ := onboardingOutboundNote(outboundOperation)
			client := nativeFixture(t, func(r *http.Request, _ map[string]any) (any, error) {
				reads++
				if r.URL.Path == "/friend/get_designated_friends" {
					return outboundFriendResponse(mode == "friends"), nil
				}
				if r.URL.Path != "/friend/get_self_friend_apply_list" {
					t.Fatal("reconciliation mutated or searched another identity", r.URL.Path)
				}
				rows := []any{}
				row := outboundApplication(note, 1700000000)
				switch mode {
				case "missing", "incomplete":
				case "unrelated":
					row["reqMsg"] = "historical unrelated request"
					rows = append(rows, row)
				case "ambiguous":
					rows = append(rows, row, outboundApplication(note, 1700000001))
				case "duplicate":
					rows = append(rows, row, row)
				case "reverse":
					row["fromUserID"], row["toUserID"] = "target-string", outboundSession.UserID
					rows = append(rows, row)
				case "wrong_target":
					row["toUserID"] = "another-string"
					rows = append(rows, row)
				default:
					if mode == "approved" {
						row["handleResult"] = 1
					}
					if mode == "rejected" {
						row["handleResult"] = -1
					}
					if mode == "wrong_state" {
						row["handleResult"] = 2
					}
					rows = append(rows, row)
				}
				result := map[string]any{"friendRequests": rows, "total": len(rows)}
				if mode == "incomplete" {
					result["total"] = 101
				}
				if mode == "missing_list" {
					delete(result, "friendRequests")
				}
				return result, nil
			})
			result, err := client.ReconcileOnboardingOutboundFriend(context.Background(), outboundSession,
				OnboardingOutboundRequest{OperationID: outboundOperation, SelfID: outboundSession.UserID,
					Target: OnboardingOutboundTarget{ID: "target-string", Account: "native-account"}})
			switch mode {
			case "ambiguous", "reverse", "duplicate", "incomplete", "missing_list", "wrong_state":
				if err == nil {
					t.Fatal("unverifiable original request admitted", result)
				}
			case "friends":
				if err != nil || reads != 1 || !result.IsFriend || result.State != "friends" || result.ApplicationID != "" {
					t.Fatal("friend state invented an application or reread history", result, err)
				}
			default:
				expected := mode
				if mode == "approved" {
					expected = "unknown"
				}
				if mode == "missing" || mode == "unrelated" || mode == "wrong_target" {
					expected = "missing"
				}
				if err != nil || result.State != expected || result.IsFriend || reads != 2 || result.OperationID != outboundOperation ||
					result.SenderID != outboundSession.UserID || result.RecipientID != "target-string" {
					t.Fatal("unrelated history or response changed original operation", result, err)
				}
				if expected != "missing" && result.ApplicationID != "1700000000" {
					t.Fatal("native application time lost")
				}
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), note) || strings.Contains(string(raw), "reqMsg") {
				t.Fatal("operation note escaped evidence")
			}
		})
	}
}

func TestOnboardingOutboundChiliInvalidOperationNeverReads(t *testing.T) {
	for _, operation := range []string{"", "not-a-uuid", strings.ToUpper(outboundOperation),
		"12345678_9abc-4def-8123-123456789abc", "12345678-9abc-4def-8123-123456789abz",
		"00000000-0000-0000-0000-000000000000"} {
		calls := 0
		client := nativeFixture(t, func(*http.Request, map[string]any) (any, error) { calls++; return nil, nil })
		target := OnboardingOutboundTarget{ID: "target-string", Account: "native-account"}
		req := OnboardingOutboundRequest{OperationID: operation, SelfID: outboundSession.UserID,
			Target: target, ExpiresAt: time.Now().Add(time.Minute)}
		if _, err := client.SendOnboardingOutboundFriend(context.Background(), outboundSession, req,
			func(ctx context.Context, _ OnboardingOutboundTarget, send func(context.Context) error) error {
				return send(ctx)
			}); err == nil {
			t.Fatal("invalid operation admitted")
		}
		if _, err := client.ReconcileOnboardingOutboundFriend(context.Background(), outboundSession, req); err == nil || calls != 0 {
			t.Fatal("invalid original operation reached network")
		}
	}
}

func TestOnboardingOutboundChiliRejectsAccountReplacementAndFriendShape(t *testing.T) {
	for _, mode := range []string{"changed_self", "missing_friends", "wrong_friends_type", "wrong_owner", "wrong_friend", "duplicate_friend"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			client := nativeFixture(t, func(r *http.Request, _ map[string]any) (any, error) {
				calls++
				if r.URL.Path != "/friend/get_designated_friends" {
					t.Fatal("reconciliation called an unrelated route")
				}
				if mode == "missing_friends" {
					return map[string]any{"unexpected": true}, nil
				}
				if mode == "wrong_friends_type" {
					return map[string]any{"friendsInfo": map[string]any{}}, nil
				}
				owner, target := outboundSession.UserID, "target-string"
				if mode == "wrong_owner" {
					owner = "replacement-robot"
				}
				if mode == "wrong_friend" {
					target = "replacement-target"
				}
				row := map[string]any{"ownerUserID": owner,
					"friendUser": map[string]any{"userID": target, "nickname": "synthetic"}}
				rows := []any{row}
				if mode == "duplicate_friend" {
					rows = append(rows, row)
				}
				return map[string]any{"friendsInfo": rows}, nil
			})
			req := OnboardingOutboundRequest{OperationID: outboundOperation, SelfID: outboundSession.UserID,
				Target: OnboardingOutboundTarget{ID: "target-string", Account: "native-account"}, ExpiresAt: time.Now().Add(time.Minute)}
			if mode == "changed_self" {
				req.SelfID = "replacement-robot"
				if _, err := client.SendOnboardingOutboundFriend(context.Background(), outboundSession, req,
					func(ctx context.Context, _ OnboardingOutboundTarget, submit func(context.Context) error) error {
						return submit(ctx)
					}); err == nil || calls != 0 {
					t.Fatal("replaced account reached outbound submission")
				}
			}
			if _, err := client.ReconcileOnboardingOutboundFriend(context.Background(), outboundSession, req); err == nil {
				t.Fatal("cross-account or malformed friendship admitted")
			}
		})
	}
}

func TestOnboardingOutboundChiliProtoEmptyListsRemainBounded(t *testing.T) {
	calls := 0
	client := nativeFixture(t, func(r *http.Request, _ map[string]any) (any, error) {
		calls++
		switch r.URL.Path {
		case "/friend/get_designated_friends":
			return map[string]any{"friendsInfo": nil}, nil
		case "/friend/get_self_friend_apply_list":
			return map[string]any{"friendRequests": nil, "total": 0}, nil
		default:
			t.Fatal("empty native lists initiated a mutation")
			return nil, nil
		}
	})
	result, err := client.ReconcileOnboardingOutboundFriend(context.Background(), outboundSession,
		OnboardingOutboundRequest{OperationID: outboundOperation, SelfID: outboundSession.UserID,
			Target: OnboardingOutboundTarget{ID: "target-string", Account: "native-account"}})
	if err != nil || result.State != "missing" || result.IsFriend || calls != 2 {
		t.Fatal("explicit SDK empty list became unknown or a friend", result, err)
	}
}
