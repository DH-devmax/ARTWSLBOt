package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/control"
)

func contactPrepare(t *testing.T, f *onboardingFixture, client testClient, c control.GroupBindingChallenge) control.GroupBindingChallenge {
	t.Helper()
	raw := onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/friend-candidate", "contact-prepare",
		map[string]any{"version": c.Version, "nickname": "Synthetic applicant"}, 200)
	var out control.GroupBindingChallenge
	if err := json.Unmarshal(raw, &out); err != nil || out.FriendContact == nil || out.FriendContact.Status != "prepared" ||
		out.FriendContact.Name != "Synthetic applicant" || out.IdentityProven || !out.FriendContact.CanApprove {
		t.Fatal("contact selection changed identity or did not expose a single confirmation", out, err)
	}
	return out
}

func contactApprove(t *testing.T, f *onboardingFixture, client testClient, c control.GroupBindingChallenge) control.GroupBindingChallenge {
	t.Helper()
	raw := onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/friend-approve", "contact-approve",
		map[string]any{"version": c.Version, "operation_id": c.FriendContact.ID}, 200)
	var out control.GroupBindingChallenge
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func expiredContactPrepare(t *testing.T, f *onboardingFixture, n *bindingFriendNativeFixture, client testClient, platform string) control.GroupBindingChallenge {
	t.Helper()
	friendOpenGate(t, f, platform)
	n.expiredContact = true
	c := f.createChallenge(t, client, platform, "expired-contact-create")
	raw := onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/friend-candidate",
		"expired-contact-select", map[string]any{"version": c.Version, "nickname": "Synthetic applicant"}, 200)
	if err := json.Unmarshal(raw, &c); err != nil || c.FriendContact == nil ||
		c.FriendContact.Action != "reject_expired" || !c.FriendContact.CanReject || c.FriendContact.CanApprove ||
		c.FriendContact.Status != "prepared" || c.IdentityProven || n.rejections.Load() != 0 || n.submits.Load() != 0 {
		t.Fatal("expired contact selection submitted a decision or enabled approval", c, err)
	}
	return c
}

func TestGroupBindingContactExpiredPostgresDefaultRejection(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, ref := friendFixture(t, platform)
			c := expiredContactPrepare(t, f, n, client, platform)
			path := "/group-binding/challenges/" + c.ID
			input := control.GroupBindingContactApproval{Version: c.Version, OperationID: c.FriendContact.ID, Key: "expired-contact-confirm"}
			if _, err := f.app.GroupBinding.ApproveContact(context.Background(), ref, c.ID, input); err == nil {
				t.Fatal("expired rejection plan was approved")
			}
			body := map[string]any{"version": c.Version, "operation_id": c.FriendContact.ID}
			// No GET, duplicate preparation or missing slider can perform cleanup.
			customerBindingProgress(t, client, n, c.ID)
			status, _, code := client.requestRaw("POST", path+"/friend-reject", body,
				http.Header{"Idempotency-Key": []string{"expired-contact-without-captcha"}})
			if status != 400 || code != "captcha_required" || n.rejections.Load() != 0 || n.submits.Load() != 0 {
				t.Fatal("rejection bypassed human verification", status, code)
			}
			other := f.bob
			other.workspace = platform
			onboardingAPI(t, other, "POST", path+"/friend-reject", "expired-contact-other", body, 404)
			onboardingAPI(t, client, "POST", path+"/friend-reject", "expired-contact-forged",
				map[string]any{"version": c.Version, "operation_id": c.FriendContact.ID, "sender_id": "substituted"}, 400)
			raw := onboardingAPI(t, client, "POST", path+"/friend-reject", input.Key, body, 200)
			var out control.GroupBindingChallenge
			if err := json.Unmarshal(raw, &out); err != nil || out.State != "cancelled" ||
				out.BlockedReason != "group_binding_friend_reapply_required" ||
				out.FriendContact == nil || out.FriendContact.Status != "succeeded" ||
				out.FriendContact.Code != "native_friend_expired_rejection_acknowledged" ||
				out.FriendContact.CanApprove || out.FriendContact.CanReject || out.IdentityProven ||
				out.Version != c.Version+1 || n.approvals.Load() != 0 || n.rejections.Load() != 1 || n.submits.Load() != 1 {
				t.Fatal("expired request did not end with one rejection and a fresh-application prompt", out, err)
			}
			again, err := f.app.GroupBinding.RejectExpiredContact(context.Background(), ref, c.ID, input)
			if err != nil || again.ID != c.ID || n.rejections.Load() != 1 || n.submits.Load() != 1 {
				t.Fatal("repeated rejection retried a native decision", again, err)
			}
			customerBindingProgress(t, client, n, c.ID)
			if friendCount(t, f, `SELECT count(*) FROM group_binding_friend_proofs`) != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_challenges WHERE identity_sealed IS NOT NULL`) != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_bindings`) != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_friend_attempts WHERE attempts_used=1`) != 1 {
				t.Fatal("cleanup established ownership or failed to consume its single attempt")
			}
			f.createChallenge(t, client, platform, "expired-contact-restart")
		})
	}
}

func TestGroupBindingContactExpiredPostgresUnknownAndFinalFences(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, scenario := range []string{"unknown_rejected", "unknown_pending", "gate_closed", "target_changed", "fresh_target", "receiver_changed", "restart"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				f, n, client, ref := friendFixture(t, platform)
				c := expiredContactPrepare(t, f, n, client, platform)
				in := control.GroupBindingContactApproval{Version: c.Version, OperationID: c.FriendContact.ID, Key: "expired-contact-fenced"}
				n.unknown = strings.HasPrefix(scenario, "unknown")
				n.reconcileState = strings.TrimPrefix(scenario, "unknown_")
				n.beforeApproval = func(ctx context.Context, r control.GroupBindingFriendApproval) error {
					var err error
					switch scenario {
					case "gate_closed":
						_, err = f.db.Platform.Exec(ctx, `UPDATE group_binding_friend_gates SET state='closed',version=version+1 WHERE account_id=$1`, r.AccountID)
					case "receiver_changed":
						_, err = f.db.Platform.Exec(ctx, `UPDATE account_receivers SET revision=revision+1 WHERE account_id=$1`, r.AccountID)
					}
					return err
				}
				n.finalEvidence = func(e *control.GroupBindingFriendEvidence) {
					if scenario == "target_changed" {
						e.ApplicationID = "different-old-application"
					}
					if scenario == "fresh_target" {
						e.RequestedAt = time.Now()
					}
				}
				if scenario == "restart" {
					if err := f.app.GroupBinding.RecoverFriends(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				out, err := f.app.GroupBinding.RejectExpiredContact(context.Background(), ref, c.ID, in)
				if err != nil || out.IdentityProven || out.FriendContact == nil {
					t.Fatal(out, err)
				}
				wantSubmits := int32(0)
				if n.unknown {
					wantSubmits = 1
					if out.FriendContact.Status != "unknown" || out.CanCancel || out.State != "waiting" {
						t.Fatal("unknown rejection released its reservation", out)
					}
					again, err := f.app.GroupBinding.RejectExpiredContact(context.Background(), ref, c.ID, in)
					if err != nil || again.FriendContact.Status != "unknown" || n.submits.Load() != 1 {
						t.Fatal("unknown rejection retried", again, err)
					}
					result, err := f.app.GroupBinding.ReconcileFriend(context.Background(), ref, c.ID, in.OperationID)
					if err != nil || n.submits.Load() != 1 {
						t.Fatal(result, err)
					}
					if scenario == "unknown_rejected" && (result.Status != "succeeded" || result.Code != "native_friend_expired_rejection_observed") ||
						scenario == "unknown_pending" && result.Status != "unknown" {
						t.Fatal("rejection reconciliation guessed a terminal state", result)
					}
					raw := onboardingAPI(t, client, "GET", "/group-binding/challenges/"+c.ID, "", nil, 200)
					if err := json.Unmarshal(raw, &out); err != nil {
						t.Fatal(err)
					}
					if scenario == "unknown_rejected" && out.State != "cancelled" ||
						scenario == "unknown_pending" && out.State != "waiting" {
						t.Fatal("reconciliation released an unresolved operation", out)
					}
				} else if out.FriendContact.Status != "failed" {
					t.Fatal("failed fence left a submittable decision", out)
				}
				if n.submits.Load() != wantSubmits || n.approvals.Load() != 0 ||
					friendCount(t, f, `SELECT count(*) FROM group_binding_friend_proofs`) != 0 ||
					friendCount(t, f, `SELECT count(*) FROM group_binding_challenges WHERE identity_sealed IS NOT NULL`) != 0 {
					t.Fatal("cleanup crossed its effect or identity boundary")
				}
			})
		}
	}
}

func TestGroupBindingContactExpiredPostgresCannotRejectFreshCandidate(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, ref := friendFixture(t, platform)
			friendOpenGate(t, f, platform)
			c := contactPrepare(t, f, client, f.createChallenge(t, client, platform, "fresh-reject-create"))
			_, err := f.app.GroupBinding.RejectExpiredContact(context.Background(), ref, c.ID,
				control.GroupBindingContactApproval{Version: c.Version, OperationID: c.FriendContact.ID, Key: "fresh-reject-forged"})
			friendFailure(t, err, 409, "group_binding_friend_decision_changed")
			if n.rejections.Load() != 0 || n.approvals.Load() != 0 || n.submits.Load() != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_friend_attempts WHERE attempts_used<>0`) != 0 {
				t.Fatal("rejection endpoint consumed or modified a fresh candidate")
			}
		})
	}
}

func TestGroupBindingContactPostgresScopeAndNoIdentity(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, _ := friendFixture(t, platform)
			c := f.createChallenge(t, client, platform, "contact-scope-create")
			path := "/group-binding/challenges/" + c.ID
			onboardingAPI(t, client, "POST", path+"/friend-candidate", "contact-closed",
				map[string]any{"version": c.Version, "nickname": "Synthetic applicant"}, 409)
			if n.lookups.Load() != 0 {
				t.Fatal("closed contact admission caused an upstream lookup")
			}
			friendOpenGate(t, f, platform)
			for _, field := range []string{"sender_id", "recipient_id", "account_id", "since", "until", "challenge_text"} {
				onboardingAPI(t, client, "POST", path+"/friend-candidate", "contact-forged-field",
					map[string]any{"version": c.Version, "nickname": "Synthetic applicant", field: "browser supplied"}, 400)
			}
			member := f.member
			member.workspace = platform
			onboardingAPI(t, member, "POST", path+"/friend-candidate", "contact-member",
				map[string]any{"version": c.Version, "nickname": "Synthetic applicant"}, 403)
			other := f.bob
			other.workspace = platform
			onboardingAPI(t, other, "POST", path+"/friend-candidate", "contact-other",
				map[string]any{"version": c.Version, "nickname": "Synthetic applicant"}, 404)
			c = contactPrepare(t, f, client, c)
			before := customerBindingNativeCalls(n)
			c = contactPrepare(t, f, client, c)
			customerBindingProgress(t, client, n, c.ID)
			if customerBindingNativeCalls(n) != before {
				t.Fatal("repeated selection or GET called the native port")
			}
			onboardingAPI(t, other, "POST", path+"/friend-approve", "contact-other-approve",
				map[string]any{"version": c.Version, "operation_id": c.FriendContact.ID}, 404)
			c = contactApprove(t, f, client, c)
			if c.IdentityProven || c.CanConfirm || c.Group != nil || c.FriendContact.Status != "succeeded" ||
				n.submits.Load() != 1 || n.approvals.Load() != 1 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_friend_proofs`) != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_challenges WHERE identity_sealed IS NOT NULL`) != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_bindings`) != 0 {
				t.Fatal("friend approval was treated as identity or group proof", c)
			}
			before = customerBindingNativeCalls(n)
			contactApprove(t, f, client, c)
			if customerBindingNativeCalls(n) != before {
				t.Fatal("repeated confirmation approved twice")
			}
			var audit string
			if err := f.db.Platform.QueryRow(context.Background(), `SELECT coalesce(json_agg(detail)::text,'')
				FROM audit_events WHERE action IN ('group_binding_contact_selected','group_binding_contact_approved')`).Scan(&audit); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(audit, "Synthetic applicant") || strings.Contains(audit, n.owner) {
				t.Fatal("contact name or native identity escaped into audit metadata")
			}
		})
	}
}

func TestGroupBindingContactPostgresConcurrentActorRateLimitAcrossPlatforms(t *testing.T) {
	f, n, _, _ := friendFixture(t, "wsl")
	ctx := context.Background()
	n.noFence = true
	var candidates []control.GroupBindingChallenge
	var clients []testClient
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		client := f.alice
		client.workspace = platform
		friendOpenGate(t, f, platform)
		prepare := func(c control.GroupBindingChallenge, key string) control.GroupBindingChallenge {
			raw := onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/friend-candidate", key,
				map[string]any{"version": c.Version, "nickname": "Synthetic applicant"}, 200)
			var out control.GroupBindingChallenge
			if err := json.Unmarshal(raw, &out); err != nil || out.FriendContact == nil ||
				out.FriendContact.Status != "prepared" || !out.FriendContact.CanApprove {
				t.Fatal("rate-limit fixture did not prepare a contact", out, err)
			}
			return out
		}
		history := prepare(f.createChallenge(t, client, platform, "contact-rate-history-"+platform),
			"contact-rate-history-select-"+platform)
		history = contactApprove(t, f, client, history)
		if history.FriendContact == nil || history.FriendContact.Status != "failed" || n.submits.Load() != 0 {
			t.Fatal("failed historical confirmation did not retain its attempt without a native effect", history)
		}
		onboardingAPI(t, client, "POST", "/group-binding/challenges/"+history.ID+"/cancel", "contact-rate-cancel-"+platform,
			map[string]any{"version": history.Version}, 200)
		time.Sleep(5100 * time.Millisecond)
		candidates = append(candidates,
			prepare(f.createChallenge(t, client, platform, "contact-rate-concurrent-"+platform),
				"contact-rate-concurrent-select-"+platform))
		clients = append(clients, client)
	}
	if n.approvals.Load() != 2 || friendCount(t, f, `SELECT count(*) FROM group_binding_friend_attempts
		WHERE actor_id=$1 AND attempts_used=1 AND updated_at>clock_timestamp()-interval '1 hour'`, f.aliceID) != 2 {
		t.Fatal("concurrent actor fixture must start with two consumed attempts")
	}

	// Hold both candidate rows so concurrent confirmations reach the reservation
	// boundary before either can commit, without manufacturing consumed attempts.
	blocker, err := f.db.Platform.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	var blockerPID, locked int
	if err = blocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	if err = blocker.QueryRow(ctx, `SELECT count(*) FROM (
		SELECT id FROM group_binding_friend_attempts WHERE id IN ($1,$2) FOR UPDATE
	) locked`, candidates[0].FriendContact.ID, candidates[1].FriendContact.ID).Scan(&locked); err != nil || locked != 2 {
		t.Fatal("could not hold both prepared candidates", locked, err)
	}
	type confirmation struct {
		status int
		data   json.RawMessage
		code   string
	}
	results := make(chan confirmation, 2)
	start := make(chan struct{})
	for i, candidate := range candidates {
		go func(client testClient, c control.GroupBindingChallenge) {
			<-start
			status, data, code := client.requestWithCaptcha("POST", "/group-binding/challenges/"+c.ID+"/friend-approve",
				map[string]any{"version": c.Version, "operation_id": c.FriendContact.ID},
				http.Header{"Idempotency-Key": {"contact-rate-concurrent-confirm-" + c.Platform}})
			results <- confirmation{status: status, data: data, code: code}
		}(clients[i], candidate)
	}
	close(start)
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		var blocked int
		// Both requests must be blocked, directly on a prepared row or through
		// the transaction already waiting for it. This also exposes the old race.
		err = f.db.Platform.QueryRow(wait, `WITH RECURSIVE blocked(pid) AS (
			SELECT pid FROM pg_stat_activity WHERE datname=current_database()
				AND $1=ANY(pg_blocking_pids(pid))
			UNION
			SELECT a.pid FROM pg_stat_activity a JOIN blocked b ON b.pid=ANY(pg_blocking_pids(a.pid))
				WHERE a.datname=current_database()
		) SELECT count(*) FROM blocked`, blockerPID).Scan(&blocked)
		if err != nil {
			t.Fatal("concurrent confirmations did not reach the reservation barrier", err)
		}
		if blocked == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n.approvals.Load() != 2 {
		t.Fatal("native port ran before the reservation committed")
	}
	if err = blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	accepted, limited := 0, 0
	for range 2 {
		select {
		case result := <-results:
			switch result.status {
			case http.StatusOK:
				var out control.GroupBindingChallenge
				if err := json.Unmarshal(result.data, &out); err != nil || out.FriendContact == nil ||
					out.FriendContact.Status != "failed" || out.IdentityProven {
					t.Fatal("reserved attempt did not retain the no-fence failure", out, err)
				}
				accepted++
			case http.StatusTooManyRequests:
				if result.code != "group_binding_friend_rate_limited" {
					t.Fatal("confirmation reached an unrelated rate limit", result.code)
				}
				limited++
			default:
				t.Fatal("unexpected concurrent confirmation result", result.status, result.code)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent contact confirmations did not finish")
		}
	}
	if accepted != 1 || limited != 1 || n.approvals.Load() != 3 || n.submits.Load() != 0 ||
		friendCount(t, f, `SELECT count(*) FROM group_binding_friend_attempts
			WHERE actor_id=$1 AND attempts_used=1 AND updated_at>clock_timestamp()-interval '1 hour'`, f.aliceID) != 3 ||
		friendCount(t, f, `SELECT count(*) FROM group_binding_friend_attempts
			WHERE id IN ($1,$2) AND status='prepared' AND attempts_used=0`,
			candidates[0].FriendContact.ID, candidates[1].FriendContact.ID) != 1 {
		t.Fatal("concurrent platforms exceeded the actor quota or lost the unreserved candidate",
			accepted, limited, n.approvals.Load(), n.submits.Load())
	}
}

func TestGroupBindingContactPostgresFinalFences(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, scenario := range []string{"gate_closed", "actor_revoked", "account_epoch", "receiver_revision", "target_changed", "no_fence", "double_fence", "unknown", "unknown_rejected"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				f, n, client, _ := friendFixture(t, platform)
				friendOpenGate(t, f, platform)
				c := contactPrepare(t, f, client, f.createChallenge(t, client, platform, "contact-final-create"))
				n.beforeApproval = func(ctx context.Context, r control.GroupBindingFriendApproval) error {
					var err error
					switch scenario {
					case "gate_closed":
						_, err = f.db.Platform.Exec(ctx, `UPDATE group_binding_friend_gates SET state='closed',version=version+1 WHERE account_id=$1`, r.AccountID)
					case "actor_revoked":
						_, err = f.db.Platform.Exec(ctx, `UPDATE users SET access_version=access_version+1 WHERE id=$1`, f.aliceID)
					case "account_epoch":
						_, err = f.db.Platform.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, r.AccountID)
					case "receiver_revision":
						_, err = f.db.Platform.Exec(ctx, `UPDATE account_receivers SET revision=revision+1 WHERE account_id=$1`, r.AccountID)
					}
					return err
				}
				n.noFence = scenario == "no_fence"
				n.duplicateFence = scenario == "double_fence"
				n.unknown = scenario == "unknown" || scenario == "unknown_rejected"
				if scenario == "unknown_rejected" {
					n.reconcileState = "rejected"
				}
				if scenario == "target_changed" {
					n.finalEvidence = func(e *control.GroupBindingFriendEvidence) { e.ApplicationID = "substituted-application" }
				}
				got, err := f.app.GroupBinding.ApproveContact(context.Background(), onboardingRef(t, client, platform), c.ID,
					control.GroupBindingContactApproval{Version: c.Version, OperationID: c.FriendContact.ID, Key: "contact-final-confirm"})
				if scenario == "actor_revoked" {
					if err == nil {
						t.Fatal("revoked scope was delivered a result")
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if scenario == "account_epoch" && (got.BlockedReason != "group_binding_scope_changed" || got.FriendContact != nil) {
					t.Fatal("changed account exposed the sealed contact", got)
				}
				want := int32(0)
				if scenario == "unknown" || scenario == "unknown_rejected" || scenario == "double_fence" {
					want = 1
				}
				if n.submits.Load() != want || friendCount(t, f, `SELECT count(*) FROM group_binding_challenges WHERE identity_sealed IS NOT NULL`) != 0 {
					t.Fatal("final fence lost its side effect or identity boundary", n.submits.Load(), want)
				}
				if scenario == "unknown" || scenario == "unknown_rejected" || scenario == "double_fence" {
					ref := onboardingRef(t, client, platform)
					old := n.submits.Load()
					got, err := f.app.GroupBinding.ReconcileFriend(context.Background(), ref, c.ID, c.FriendContact.ID)
					wantState := "succeeded"
					if scenario == "unknown_rejected" {
						wantState = "failed"
					}
					if err != nil || got.Status != wantState || n.submits.Load() != old {
						t.Fatal("unknown contact did not reconcile without replay", got, err)
					}
					if friendCount(t, f, `SELECT count(*) FROM group_binding_challenges WHERE identity_sealed IS NOT NULL`) != 0 {
						t.Fatal("contact reconciliation granted identity")
					}
				}
			})
		}
	}
}

func TestGroupBindingContactPostgresDelayedConfirmation(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, _ := friendFixture(t, platform)
			friendOpenGate(t, f, platform)
			c := contactPrepare(t, f, client, f.createChallenge(t, client, platform, "contact-delay-create"))
			// A person can spend longer than the native evidence freshness
			// interval reviewing the candidate and completing the slider.
			time.Sleep(5500 * time.Millisecond)
			c = contactApprove(t, f, client, c)
			if c.FriendContact.Status != "succeeded" || n.submits.Load() != 1 || c.IdentityProven {
				t.Fatal("confirmation reused stale selection evidence instead of rereading", c)
			}
		})
	}
}

func TestGroupBindingContactPostgresExpiryReleasesUnsubmitted(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, _ := friendFixture(t, platform)
			friendOpenGate(t, f, platform)
			original := f.createChallenge(t, client, platform, "contact-expiry-original")
			onboardingAPI(t, client, "POST", "/group-binding/challenges/"+original.ID+"/cancel", "contact-expiry-cancel",
				map[string]any{"version": original.Version}, 200)
			id := control.NewID()
			// A short-lived synthetic challenge exercises real expiry without
			// changing immutable deadlines or waiting ten minutes.
			_, err := f.db.Platform.Exec(context.Background(), `INSERT INTO group_binding_challenges(
				id,tenant_id,applicant_id,applicant_mode,platform,account_id,account_epoch,account_version,
				applicant_version,token_hash,private_token_hash,create_key,request_hash,
				created_at,expires_at,reservation_until)
				SELECT $1,tenant_id,applicant_id,applicant_mode,platform,account_id,account_epoch,account_version,
				applicant_version,$2,$3,$4,request_hash,clock_timestamp(),
				clock_timestamp()+interval '6 seconds',clock_timestamp()+interval '6 seconds'
				FROM group_binding_challenges WHERE id=$5`, id, bytes.Repeat([]byte{90}, 32),
				bytes.Repeat([]byte{107}, 32), "contact-expiry-"+id, original.ID)
			if err != nil {
				t.Fatal(err)
			}
			raw := onboardingAPI(t, client, "GET", "/group-binding/challenges/"+id, "", nil, 200)
			var c control.GroupBindingChallenge
			if err := json.Unmarshal(raw, &c); err != nil {
				t.Fatal(err)
			}
			c = contactPrepare(t, f, client, c)
			before := customerBindingNativeCalls(n)
			time.Sleep(time.Until(c.ExpiresAt) + 25*time.Millisecond)
			if err := f.app.GroupBinding.ScanOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			var state, operation string
			if err := f.db.Platform.QueryRow(context.Background(), `SELECT c.state,a.status
				FROM group_binding_challenges c JOIN group_binding_friend_attempts a ON a.challenge_id=c.id
				WHERE c.id=$1`, id).Scan(&state, &operation); err != nil {
				t.Fatal(err)
			}
			if state != "expired" || operation != "failed" || customerBindingNativeCalls(n) != before {
				t.Fatal("expired unsubmitted contact retained its slot or invoked native code", state, operation)
			}
			f.createChallenge(t, client, platform, "contact-expiry-replacement")
		})
	}
}

func TestGroupBindingContactPostgresLateRead(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, ref := friendFixture(t, platform)
			friendOpenGate(t, f, platform)
			c := f.createChallenge(t, client, platform, "contact-late-create")
			n.beforeLookup = func() {
				if _, err := f.db.Platform.Exec(context.Background(), `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, c.Robot.ID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.app.GroupBinding.PrepareContact(context.Background(), ref, c.ID,
				control.GroupBindingContactInput{Version: c.Version, Nickname: "Synthetic applicant", Key: "contact-late-prepare"}); err == nil {
				t.Fatal("late selection result crossed an account epoch")
			}
			if friendCount(t, f, `SELECT count(*) FROM group_binding_friend_attempts`) != 0 || n.submits.Load() != 0 {
				t.Fatal("late read persisted an operation or sent an approval")
			}
		})
	}
}

func TestGroupBindingContactPostgresCancelAndRecovery(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, scenario := range []string{"cancel", "restart_prepared", "restart_submitting", "private_proof"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				f, n, client, ref := friendFixture(t, platform)
				friendOpenGate(t, f, platform)
				created := f.createChallenge(t, client, platform, "contact-lifecycle-create")
				c := contactPrepare(t, f, client, created)
				before := customerBindingNativeCalls(n)
				if scenario == "cancel" {
					onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/cancel", "contact-cancel",
						map[string]any{"version": c.Version}, 200)
				} else if scenario == "private_proof" {
					f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: n.owner, Text: created.PrivateChallenge}, nil)
					if err := f.app.GroupBinding.ScanOnce(context.Background()); err != nil {
						t.Fatal(err)
					}
				} else {
					if scenario == "restart_submitting" {
						if _, err := f.db.Platform.Exec(context.Background(), `UPDATE group_binding_friend_attempts
							SET status='submitting',attempts_used=1 WHERE id=$1`, c.FriendContact.ID); err != nil {
							t.Fatal(err)
						}
					}
					for range 2 {
						if err := f.app.GroupBinding.RecoverFriends(context.Background()); err != nil {
							t.Fatal(err)
						}
					}
				}
				if customerBindingNativeCalls(n) != before {
					t.Fatal("cancellation or recovery called the native port")
				}
				var state string
				if err := f.db.Platform.QueryRow(context.Background(), `SELECT status FROM group_binding_friend_attempts
					WHERE id=$1`, c.FriendContact.ID).Scan(&state); err != nil {
					t.Fatal(err)
				}
				want := "failed"
				if scenario == "restart_submitting" {
					want = "unknown"
					onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/cancel", "contact-unresolved-cancel",
						map[string]any{"version": c.Version}, 409)
				}
				identities := 0
				if scenario == "private_proof" {
					identities = 1
				}
				if state != want || friendCount(t, f, `SELECT count(*) FROM group_binding_challenges WHERE identity_sealed IS NOT NULL`) != identities {
					t.Fatal("contact lifecycle granted identity or retained dispatchable work", state)
				}
				_, err := f.app.GroupBinding.ApproveContact(context.Background(), ref, c.ID,
					control.GroupBindingContactApproval{Version: c.Version, OperationID: c.FriendContact.ID, Key: "contact-after-lifecycle"})
				if scenario != "cancel" && scenario != "private_proof" && err != nil {
					t.Fatal(err)
				}
				if n.submits.Load() != 0 {
					t.Fatal("old contact was submitted after cancellation or restart")
				}
			})
		}
	}
}
