//go:build sodium && cgo

package nativeauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOnboardingFriendWSLNativeRereadFenceAndNoReplay(t *testing.T) {
	for _, mode := range []string{"approved", "note_changed", "sender_changed", "id_changed", "processed",
		"ambiguous", "revoked", "expired", "expired_in_fence", "cancelled_in_fence", "unknown", "duplicate_submit"} {
		t.Run(mode, func(t *testing.T) {
			base, err := NewClient(syntheticDeployment())
			if err != nil {
				t.Fatal(err)
			}
			client := base.(*Client)
			defer client.Close()
			client.jwt, client.fields[3] = "synthetic-onboarding-jwt", 7
			const code = "proof-synthetic-wsl"
			reads, writes, fences := 0, 0, 0
			rows := []map[string]any{onboardingFriendRow("11", "8", code)}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _, params := desktopRequest(t, r, client.bodyKey)
				switch r.URL.Path {
				case "/v1/friend/friend-apply-list":
					reads++
					if len(params) != 1 || string(params["lastId"]) != `"0"` {
						t.Error("proof read escaped first desktop window")
					}
					json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"list": rows}})
				case "/v1/friend/friend-apply-handler":
					writes++
					if fences != 1 || len(params) != 5 || string(params["flId"]) != `"11"` ||
						string(params["state"]) != `"FRIENDLOG_STATE_AGREE"` {
						t.Error("approval escaped original application or fence")
					}
					if mode == "unknown" {
						w.WriteHeader(http.StatusBadGateway)
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
						"flId": "11", "state": "FRIENDLOG_STATE_AGREE"}})
				default:
					t.Error("unreviewed onboarding route", r.URL.Path)
				}
			}))
			defer server.Close()
			client.config.Origin, client.http = server.URL, server.Client()
			proof, err := client.OnboardingFriend(context.Background(), code)
			if err != nil || reads != 1 || writes != 0 {
				t.Fatal("preparation performed extra reads or a mutation", err)
			}
			switch mode {
			case "note_changed":
				rows[0]["applyMsg"] = code + " changed"
			case "sender_changed":
				rows[0]["user"] = map[string]any{"uid": "9"}
			case "id_changed":
				rows[0]["flId"] = "12"
			case "processed":
				rows[0]["state"] = "FRIENDLOG_STATE_AGREE"
			case "ambiguous":
				rows = append(rows, onboardingFriendRow("12", "9", code))
			}
			deadline := time.Now().Add(time.Minute)
			if mode == "expired" {
				deadline = time.Now().Add(-time.Second)
			}
			if mode == "expired_in_fence" {
				deadline = time.Now().Add(50 * time.Millisecond)
			}
			err = client.ApproveOnboardingFriend(context.Background(), code, proof, deadline,
				func(work context.Context, current OnboardingFriendEvidence, submit func(context.Context) error) error {
					fences++
					if current != proof || reads != 2 {
						t.Fatal("cached or changed proof reached final fence")
					}
					if mode == "revoked" {
						return errors.New("synthetic-revoked")
					}
					if mode == "expired_in_fence" {
						timer := time.NewTimer(time.Until(deadline) + time.Millisecond)
						<-timer.C
					}
					if mode == "cancelled_in_fence" {
						cancelled, cancel := context.WithCancel(work)
						cancel()
						return submit(cancelled)
					}
					err := submit(work)
					if mode == "duplicate_submit" {
						return submit(work)
					}
					return err
				})
			if mode == "approved" {
				if err != nil || writes != 1 || fences != 1 {
					t.Fatal("approval did not submit exactly once", err, writes, fences)
				}
				rows[0]["state"] = "FRIENDLOG_STATE_AGREE"
				result, err := client.ReconcileOnboardingFriend(context.Background(), code, proof)
				if err != nil || result.State != "approved" || writes != 1 || reads != 3 {
					t.Fatal("read-only reconciliation repeated approval", err)
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
				t.Fatal("unauthorized mutation or automatic retry", writes)
			}
			if mode == "unknown" {
				var native *Error
				if !errors.As(err, &native) || !native.Submitted {
					t.Fatal("unknown submission lost submitted status", err)
				}
			}
		})
	}
}
