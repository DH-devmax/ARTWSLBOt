//go:build sodium && cgo

package nativeauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOnboardingContactWSLNativeRereadFenceAndExactlyOneApproval(t *testing.T) {
	testOnboardingContactWSLNativeChange(t, false)
}

func TestOnboardingContactWSLNativeRejectExpiredRereadFenceAndNoReplay(t *testing.T) {
	testOnboardingContactWSLNativeChange(t, true)
}

func testOnboardingContactWSLNativeChange(t *testing.T, rejectExpired bool) {
	t.Helper()
	modes := []string{"approved", "name_changed", "timestamp_changed", "timestamp_missing",
		"sender_changed", "id_changed", "outgoing", "self", "processed", "refused", "native_expired",
		"ambiguous", "revoked", "expired", "wrong_action",
		"nil_fence", "no_submit", "expired_in_fence", "cancelled_final", "cancelled_parent",
		"cancelled_before", "unknown", "duplicate_submit", "concurrent_submit"}
	if rejectExpired {
		modes = append(modes, "flag_disabled", "expected_name_missing", "expected_time_missing", "new_current")
	} else {
		modes = append(modes, "current_with_expired")
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			base, err := NewClient(syntheticDeployment())
			if err != nil {
				t.Fatal(err)
			}
			client := base.(*Client)
			defer client.Close()
			client.jwt, client.fields[3] = "synthetic-contact-jwt", 7
			selector, at := onboardingContactFixture()
			selector.RejectExpired = rejectExpired || mode == "wrong_action" || mode == "current_with_expired"
			if rejectExpired && mode != "wrong_action" || !rejectExpired && mode == "wrong_action" {
				at = selector.Since.Add(-10 * time.Minute)
			}
			wireState := "FRIENDLOG_STATE_AGREE"
			if rejectExpired {
				wireState = "FRIENDLOG_STATE_REFUSE"
			}
			var reads, writes, fences atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _, params := desktopRequest(t, r, client.bodyKey)
				if r.Header.Get("X-Device") != "1" {
					t.Error("incoming contact used a non-desktop request")
				}
				switch r.URL.Path {
				case "/v1/friend/friend-apply-list":
					n := reads.Add(1)
					if len(params) != 1 || string(params["lastId"]) != `"0"` {
						t.Error("contact read escaped first desktop window")
					}
					row := onboardingContactRow("11", "8", selector.Name, at)
					rows := []map[string]any{row}
					if n > 1 {
						switch mode {
						case "name_changed":
							row["user"] = map[string]any{"uid": "8", "nickName": "changed"}
						case "timestamp_changed":
							row["applyTime"] = at.Add(time.Nanosecond).Format(time.RFC3339Nano)
						case "timestamp_missing":
							delete(row, "applyTime")
						case "sender_changed":
							row["user"] = map[string]any{"uid": "9", "nickName": selector.Name}
						case "id_changed":
							row["flId"] = "12"
						case "outgoing":
							row["direction"] = "FROM_ME"
						case "self":
							row["user"] = map[string]any{"uid": "7", "nickName": selector.Name}
						case "processed":
							row["state"] = "FRIENDLOG_STATE_AGREE"
						case "refused":
							row["state"] = "FRIENDLOG_STATE_REFUSE"
						case "native_expired":
							row["state"] = "FRIENDLOG_STATE_EXPIRE"
						case "ambiguous":
							rows = append(rows, onboardingContactRow("12", "9", selector.Name, at))
						case "new_current":
							rows = append(rows, onboardingContactRow("12", "9", selector.Name, selector.Since))
						}
					}
					if mode == "current_with_expired" {
						rows = append(rows, onboardingContactRow("12", "9", selector.Name, selector.Since.Add(-10*time.Minute)))
					}
					if n == 3 {
						row["state"] = wireState
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"list": rows}})
				case "/v1/friend/friend-apply-handler":
					writes.Add(1)
					if fences.Load() != 1 || len(params) != 5 || string(params["flId"]) != `"11"` ||
						string(params["state"]) != `"`+wireState+`"` ||
						string(params["nickName"]) != `"synthetic contact"` {
						t.Error("approval escaped the selected application or native fence")
					}
					if mode == "unknown" {
						w.WriteHeader(http.StatusBadGateway)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
						"flId": "11", "state": wireState}})
				default:
					t.Error("unreviewed incoming contact route", r.URL.Path)
				}
			}))
			defer server.Close()
			client.config.Origin, client.http = server.URL, server.Client()
			proof, err := client.OnboardingContact(context.Background(), selector)
			if err != nil || reads.Load() != 1 || writes.Load() != 0 || !selector.Matches(proof) {
				t.Fatal("contact preparation mutated or lost timestamp evidence", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled_before" {
				cancel()
			}
			deadline := time.Now().Add(time.Minute)
			if mode == "expired" {
				deadline = time.Now().Add(-time.Second)
			}
			if mode == "expired_in_fence" {
				deadline = time.Now().Add(40 * time.Millisecond)
			}
			var fence OnboardingFriendFence = func(work context.Context, current OnboardingFriendEvidence, send func(context.Context) error) error {
				fences.Add(1)
				if current != proof || reads.Load() != 2 {
					t.Fatal("changed or cached contact reached the final fence")
				}
				switch mode {
				case "revoked":
					return errors.New("synthetic-revoked")
				case "no_submit":
					return nil
				case "expired_in_fence":
					<-work.Done()
				case "cancelled_final":
					stopped, stop := context.WithCancel(work)
					stop()
					return send(stopped)
				case "cancelled_parent":
					cancel()
					return send(context.Background())
				case "concurrent_submit":
					var wg sync.WaitGroup
					var success atomic.Int32
					for range 8 {
						wg.Go(func() {
							if send(work) == nil {
								success.Add(1)
							}
						})
					}
					wg.Wait()
					if success.Load() != 1 {
						t.Fatal("concurrent approval did not submit exactly once")
					}
					return nil
				}
				err := send(work)
				if mode == "duplicate_submit" {
					return send(work)
				}
				return err
			}
			if mode == "nil_fence" {
				fence = nil
			}
			expected := proof
			switch mode {
			case "flag_disabled":
				selector.RejectExpired = false
			case "expected_name_missing":
				expected.Name = ""
			case "expected_time_missing":
				expected.RequestedAt = time.Time{}
			}
			if rejectExpired {
				err = client.RejectOnboardingContact(ctx, selector, expected, deadline, fence)
			} else {
				err = client.ApproveOnboardingContact(ctx, selector, expected, deadline, fence)
			}
			success := mode == "approved" || mode == "concurrent_submit" ||
				mode == "current_with_expired" || mode == "new_current"
			wantWrites := int32(0)
			if success || mode == "unknown" || mode == "duplicate_submit" {
				wantWrites = 1
			}
			if (err == nil) != success || writes.Load() != wantWrites {
				t.Fatal("contact change bypassed the fence, retried or falsely succeeded", mode, writes.Load(), err)
			}
			if writes.Load() > 0 && (fences.Load() != 1 || reads.Load() != 2) {
				t.Fatal("contact change did not reread once and fence once")
			}
			if mode == "expired" || mode == "nil_fence" {
				if reads.Load() != 1 || fences.Load() != 0 {
					t.Fatal("invalid admission performed a reread")
				}
			}
			if mode == "unknown" {
				var failure *Error
				if !errors.As(err, &failure) || !failure.Submitted {
					t.Fatal("unknown approval lost submitted evidence", err)
				}
			}
			if success || mode == "unknown" {
				result, readErr := client.ReconcileOnboardingContact(context.Background(), selector, proof)
				wantState := "approved"
				if rejectExpired {
					wantState = "rejected"
				}
				if readErr != nil || result.State != wantState || result.Name != selector.Name ||
					!result.RequestedAt.Equal(at) || reads.Load() != 3 || writes.Load() != 1 {
					t.Fatal("read-only reconciliation replayed approval or changed evidence", readErr)
				}
			}
		})
	}
}
