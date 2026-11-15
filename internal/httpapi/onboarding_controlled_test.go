package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/nativeauth"
)

func TestOnboardingControlledInputsCannotCreateAuthority(t *testing.T) {
	s := &control.GroupBindingService{}
	id := control.NewID()
	_, err := s.CreateOnboardingControlledWindow(context.Background(), control.SessionRef{},
		control.OnboardingControlledWindowInput{ChallengeID: id, GroupID: id, Version: 1,
			Account: "test-account", TargetID: "test-target", Seconds: 301, Budget: 1})
	friendFailure(t, err, 400, "invalid_test_scope")
	_, err = s.PrepareControlledOutboundFriend(context.Background(), control.SessionRef{}, id,
		control.OnboardingControlledTargetInput{Version: 1, Account: "test-account", Key: "test-prepare-key"})
	friendFailure(t, err, 400, "group_binding_outbound_invalid")
	_, err = s.SendControlledOutboundFriend(context.Background(), control.SessionRef{}, id,
		control.OnboardingControlledConfirmation{Version: 1, Key: "test-confirm-key"})
	friendFailure(t, err, 400, "group_binding_outbound_invalid")
	for _, action := range []string{"onboarding_friend_search", "onboarding_friend_request"} {
		if control.TestAction(action) || control.NativeDebugAction(action) {
			t.Fatal("ordinary Tools admitted the separate onboarding grant", action)
		}
	}
	for _, path := range []string{"friend-target", "friend-send"} {
		r := httptest.NewRequest("POST", "/api/v1/group-binding/challenges/not-an-id/"+path, nil)
		w := httptest.NewRecorder()
		friendFailure(t, retiredOutboundFriend(w, r, Principal{Role: "superadmin"}), 410,
			"group_binding_outbound_controlled_only")
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("retirement response is cacheable")
		}
	}
}

func onboardingControlledFixture(t *testing.T, platform string) (*onboardingFixture, *bindingOutboundNativeFixture,
	testClient, control.SessionRef, control.GroupBindingChallenge, control.OnboardingControlledWindow) {
	t.Helper()
	f, n, _, _ := outboundFixture(t, platform)
	ctx := context.Background()
	// All records and native observations in this fixture are synthetic.
	if _, err := f.db.Platform.Exec(ctx, `UPDATE accounts SET testing_enabled=true,connection_enabled=true
		WHERE id=$1`, f.accounts[platform]); err != nil {
		t.Fatal(err)
	}
	admin := f.admin
	admin.workspace = platform
	raw := onboardingAPI(t, admin, "POST", "/group-binding/challenges", "controlled-real-challenge-"+platform,
		map[string]any{"platform": platform, "target_tenant_id": f.tenantA}, 201)
	var c control.GroupBindingChallenge
	if err := json.Unmarshal(raw, &c); err != nil || c.FriendFlow != "incoming_contact" || c.Mode != "operator_demo" {
		t.Fatal("controlled fixture did not use a real default challenge", err, c)
	}
	var group string
	err := f.db.Platform.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name)
		VALUES($1,'synthetic-controlled-group','Synthetic controlled group') RETURNING id::text`, platform).Scan(&group)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id)
		VALUES($1,$2,$3)`, group, f.tenantA, c.Robot.ID); err != nil {
		t.Fatal(err)
	}
	in := control.OnboardingControlledWindowInput{ChallengeID: c.ID, Version: c.Version,
		GroupID: group, Account: n.target.AddAccount, TargetID: n.target.NativeID,
		Seconds: 300, Budget: 1, ConfirmIsolated: true}
	raw = onboardingAPI(t, admin, "POST", "/test-tools/onboarding-friend/windows", "", in, 201)
	var window control.OnboardingControlledWindow
	if err = json.Unmarshal(raw, &window); err != nil || window.ID == "" || window.Remaining != 1 {
		t.Fatal("controlled fixture did not create a persisted human window", err)
	}
	outboundOpenGate(t, f, platform)
	return f, n, admin, onboardingRef(t, admin, platform), c, window
}

func controlledFriendAPI(t *testing.T, client testClient, c control.GroupBindingChallenge,
	window control.OnboardingControlledWindow, action, key, account string, want int) control.GroupBindingChallenge {
	t.Helper()
	in := map[string]any{"test_window_id": window.ID, "version": c.Version}
	if action == "friend-target" {
		in["account"] = account
	}
	raw := outboundAPI(t, client, "/test-tools/onboarding-friend/challenges/"+c.ID+"/"+action, key, in, want)
	var out control.GroupBindingChallenge
	if want == 200 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestOnboardingControlledPostgresOrdinaryRoutesRetiredAfterAuthentication(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, admin, _, c, window := onboardingControlledFixture(t, platform)
			before := outboundCalls(n)
			for _, client := range []testClient{f.alice, admin} {
				client.workspace = platform
				for _, action := range []string{"friend-target", "friend-send"} {
					for _, id := range []string{c.ID, "not-an-id"} {
						status, _, code := client.requestRaw("POST", "/group-binding/challenges/"+id+"/"+action,
							map[string]any{"version": c.Version, "account": n.target.AddAccount, "test_window_id": window.ID},
							http.Header{"Idempotency-Key": {"retired-cannot-borrow-window"}})
						if status != 410 || code != "group_binding_outbound_controlled_only" {
							t.Fatal("ordinary route accepted a grant or required a new captcha", status, code)
						}
					}
				}
			}
			anonymous := newCaptchaHTTPClient(t, f.app, "127.0.0.1:4000")
			anonymous.workspace = platform
			status, _, _ := anonymous.requestRaw("POST", "/group-binding/challenges/"+c.ID+"/friend-send", nil)
			if status != 401 || outboundCalls(n) != before {
				t.Fatal("retired routes lost authentication or reached a native port", status)
			}
			for _, path := range []string{"/test-tools/onboarding-friend/windows",
				"/test-tools/onboarding-friend/challenges/" + c.ID + "/friend-target",
				"/test-tools/onboarding-friend/challenges/" + c.ID + "/friend-send"} {
				client := f.alice
				client.workspace = platform
				status, _, code := client.requestRaw("POST", path, map[string]any{})
				if status != 403 || code != "superadmin_required" || outboundCalls(n) != before {
					t.Fatal("customer reached the controlled surface", status, code)
				}
			}
		})
	}
}

func TestOnboardingControlledPostgresExplicitTargetAndSingleAttempt(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, admin, ref, c, window := onboardingControlledFixture(t, platform)
			ctx := context.Background()
			before := outboundCalls(n)
			_, err := f.app.GroupBinding.PrepareOutboundFriend(ctx, ref, c.ID,
				control.GroupBindingFriendTargetInput{Version: c.Version, Account: n.target.AddAccount, Key: "direct-incoming-forbidden"})
			friendFailure(t, err, 409, "group_binding_outbound_controlled_only")
			controlledFriendAPI(t, admin, c, window, "friend-target", "wrong-granted-account", "different-account", 409)
			if n.outboundLookups.Load() != 0 || n.outboundEffects.Load() != 0 {
				t.Fatal("a different contact selector reached search or send", before, outboundCalls(n))
			}
			prepared := controlledFriendAPI(t, admin, c, window, "friend-target", "controlled-target-prepare", n.target.AddAccount, 200)
			if prepared.FriendOutbound == nil || prepared.FriendOutbound.Status != "prepared" ||
				!prepared.FriendOutbound.CanSend || prepared.IdentityProven || prepared.Group != nil ||
				n.outboundLookups.Load() != 1 || n.outboundEffects.Load() != 0 ||
				friendCount(t, f, `SELECT count(*) FROM test_windows WHERE id=$1 AND used=0`, window.ID) != 1 {
				t.Fatal("controlled preparation sent, consumed mutation budget, or established identity", prepared)
			}
			_, err = f.app.GroupBinding.SendOutboundFriend(ctx, ref, c.ID,
				control.GroupBindingConfirmation{Version: c.Version, Key: "direct-controlled-forbidden"})
			friendFailure(t, err, 409, "group_binding_outbound_controlled_only")
			view, err := f.app.GroupBinding.Read(ctx, ref, c.ID)
			if err != nil || view.FriendOutbound == nil || view.FriendOutbound.CanSend {
				t.Fatal("customer progress inherited controlled submission authority", view, err)
			}
			sent := controlledFriendAPI(t, admin, c, window, "friend-send", "controlled-single-confirm", "", 200)
			if sent.FriendOutbound == nil || sent.FriendOutbound.Status != "submitted" || sent.IdentityProven ||
				n.outboundEffects.Load() != 1 || friendCount(t, f, `SELECT count(*) FROM test_windows WHERE id=$1 AND used=1`, window.ID) != 1 {
				t.Fatal("controlled confirmation did not consume exactly one attempt", sent)
			}
			before = outboundCalls(n)
			replay := controlledFriendAPI(t, admin, c, window, "friend-send", "controlled-single-confirm", "", 200)
			controlledFriendAPI(t, admin, c, window, "friend-send", "replacement-confirm-key", "", 409)
			if replay.FriendOutbound == nil || replay.FriendOutbound.ID != sent.FriendOutbound.ID || outboundCalls(n) != before {
				t.Fatal("repeated confirmation dispatched native work")
			}
			if friendCount(t, f, `SELECT count(*) FROM group_binding_challenges WHERE id=$1
				AND identity_sealed IS NULL AND identity_event_id IS NULL AND identity_friend_proof_id IS NULL`, c.ID) != 1 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_friend_proofs`) != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_bindings WHERE management_enabled OR ai_enabled`) != 0 {
				t.Fatal("controlled contact entered a customer proof or business runtime")
			}
			for _, secret := range []string{n.target.NativeID, n.target.AddAccount, c.PrivateChallenge} {
				if friendCount(t, f, `SELECT count(*) FROM audit_events WHERE detail::text LIKE '%'||$1||'%'`, secret) != 0 {
					t.Fatal("controlled audit exposed contact or challenge text")
				}
			}
			if _, err = f.db.Platform.Exec(ctx, `UPDATE group_binding_outbound_attempts SET test_window_id=NULL WHERE id=$1`,
				sent.FriendOutbound.ID); err == nil {
				t.Fatal("durable operation could detach its controlled window")
			}
			// The selected contact is deliberately different from the person
			// who later provides the independent authenticated identity proof.
			f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: "synthetic-independent-human",
				Text: c.PrivateChallenge}, nil)
			if err = f.app.GroupBinding.ScanOnce(ctx); err != nil {
				t.Fatal(err)
			}
			identity, err := f.app.GroupBinding.Read(ctx, ref, c.ID)
			if err != nil || !identity.IdentityProven || identity.Group != nil || identity.CanConfirm ||
				n.outboundEffects.Load() != 1 {
				t.Fatal("controlled contact selection replaced or constrained independent identity proof", identity, err)
			}
		})
	}
}

func TestOnboardingControlledPostgresHistoricalChallengeCannotExecute(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, admin, ref := outboundFixture(t, platform)
			c := outboundLegacyChallenge(t, f, admin, platform, "historical-outbound-read-only")
			before := outboundCalls(n)
			_, err := f.app.GroupBinding.PrepareOutboundFriend(context.Background(), ref, c.ID,
				control.GroupBindingFriendTargetInput{Version: c.Version, Account: n.target.AddAccount, Key: "historical-prepare-denied"})
			friendFailure(t, err, 409, "group_binding_outbound_controlled_only")
			_, err = f.app.GroupBinding.SendOutboundFriend(context.Background(), ref, c.ID,
				control.GroupBindingConfirmation{Version: c.Version, Key: "historical-send-denied"})
			friendFailure(t, err, 409, "group_binding_outbound_controlled_only")
			view, err := f.app.GroupBinding.Read(context.Background(), ref, c.ID)
			if err != nil || view.FriendFlow != "outbound" || outboundCalls(n) != before ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts WHERE challenge_id=$1`, c.ID) != 0 {
				t.Fatal("historical row retained execution authority or lost read access", view, err)
			}
		})
	}
}

func TestOnboardingControlledPostgresGrantCheckedBeforeLookup(t *testing.T) {
	for _, change := range []string{"cancel", "deadline", "budget", "actions", "targets", "binding_epoch", "tenant", "actor", "platform"} {
		t.Run(change, func(t *testing.T) {
			f, n, admin, _, c, window := onboardingControlledFixture(t, "wsl")
			var query string
			var args []any
			args = []any{window.ID}
			switch change {
			case "cancel":
				query = `UPDATE test_windows SET cancelled=true WHERE id=$1`
			case "deadline":
				query = `UPDATE test_windows SET expires_at=clock_timestamp() WHERE id=$1`
			case "budget":
				query = `UPDATE test_windows SET used=budget WHERE id=$1`
			case "actions":
				query = `UPDATE test_windows SET actions=ARRAY['send_text'] WHERE id=$1`
			case "targets":
				query = `UPDATE test_windows SET target_ids=ARRAY['other'] WHERE id=$1`
			case "binding_epoch":
				query, args = `UPDATE group_bindings SET epoch=epoch+1 WHERE group_id=$1`, []any{window.GroupID}
			case "tenant":
				query, args = `UPDATE group_bindings SET tenant_id=$2 WHERE group_id=$1`, []any{window.GroupID, f.tenantB}
			case "actor":
				query, args = `UPDATE test_windows SET actor_id=$2 WHERE id=$1`, []any{window.ID, f.aliceID}
			case "platform":
				admin.workspace = "lajiaoliao"
			}
			if query != "" {
				if _, err := f.db.Platform.Exec(context.Background(), query, args...); err != nil {
					t.Fatal(err)
				}
			}
			before := outboundCalls(n)
			status, _, code := admin.requestRaw("POST", "/test-tools/onboarding-friend/challenges/"+c.ID+"/friend-target",
				control.OnboardingControlledTargetInput{Window: window.ID, Version: c.Version, Account: n.target.AddAccount},
				http.Header{"Idempotency-Key": {"controlled-invalid-grant"}})
			if status != 409 && status != 404 || outboundCalls(n) != before ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts`) != 0 {
				t.Fatal("invalid persisted grant reached native lookup", change, status, code)
			}
		})
	}
}

func TestOnboardingControlledPostgresLookupDispatchFences(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			for _, stage := range []string{"roles", "queued"} {
				t.Run(stage, func(t *testing.T) {
					for _, change := range []string{"cancel", "deadline", "context-deadline"} {
						t.Run(change, func(t *testing.T) {
							f, n, _, ref, c, window := onboardingControlledFixture(t, platform)
							ctx, cancel := context.WithCancel(context.Background())
							if change == "context-deadline" {
								cancel()
								ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
							}
							defer cancel()
							invalidated := false
							invalidate := func(work context.Context) error {
								invalidated = true
								var query string
								switch change {
								case "cancel":
									query = `UPDATE test_windows SET cancelled=true WHERE id=$1`
								case "deadline":
									query = `UPDATE test_windows SET expires_at=clock_timestamp() WHERE id=$1`
								case "context-deadline":
									<-work.Done()
									if work.Err() != context.DeadlineExceeded {
										t.Fatal("lookup wait ended without its context deadline", work.Err())
									}
									return nil
								}
								tag, err := f.db.Platform.Exec(work, query, window.ID)
								if err == nil && tag.RowsAffected() != 1 {
									t.Fatal("lookup dispatch fixture did not invalidate its window")
								}
								return err
							}
							if stage == "roles" {
								n.beforeRoles = func() {
									if err := invalidate(ctx); err != nil {
										t.Fatal(err)
									}
								}
							} else {
								n.beforeOutboundLookup = func(work context.Context, _ control.GroupBindingOutboundLookup) error {
									deadline, ok := work.Deadline()
									if !ok || deadline.After(window.ExpiresAt) {
										t.Fatal("queued lookup did not inherit the grant deadline")
									}
									if change != "context-deadline" && !deadline.Equal(window.ExpiresAt) {
										t.Fatal("queued lookup changed the original grant deadline")
									}
									return invalidate(work)
								}
							}
							_, err := f.app.GroupBinding.PrepareControlledOutboundFriend(ctx, ref, c.ID,
								control.OnboardingControlledTargetInput{Window: window.ID, Version: c.Version,
									Account: n.target.AddAccount, Key: "controlled-dispatch-" + stage + "-" + change})
							if err == nil || !invalidated || n.outboundLookups.Load() != 0 ||
								n.outboundSends.Load() != 0 || n.outboundEffects.Load() != 0 ||
								n.outboundReconciles.Load() != 0 ||
								friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts WHERE challenge_id=$1`, c.ID) != 0 ||
								friendCount(t, f, `SELECT count(*) FROM test_windows WHERE id=$1 AND used=0`, window.ID) != 1 ||
								friendCount(t, f, `SELECT count(*) FROM audit_events
									WHERE action='group_binding_outbound_lookup_failed' AND resource_id=$1`, c.ID) != 0 {
								t.Fatal("invalidated lookup reached native query, consumed budget or persisted evidence", err)
							}
							if change == "context-deadline" {
								if ctx.Err() != context.DeadlineExceeded {
									t.Fatal("lookup returned before its request deadline", ctx.Err())
								}
							} else {
								friendFailure(t, err, 409, "native_test_window_invalid")
							}
						})
					}
				})
			}
		})
	}
}

func TestOnboardingControlledPostgresLookupMustMatchAuthorizedNativeTarget(t *testing.T) {
	f, n, admin, _, c, window := onboardingControlledFixture(t, "wsl")
	n.target.NativeID = "synthetic-ungranted-native-target"
	status, data, _ := admin.requestRaw("POST", "/test-tools/onboarding-friend/challenges/"+c.ID+"/friend-target",
		control.OnboardingControlledTargetInput{Window: window.ID, Version: c.Version, Account: n.target.AddAccount},
		http.Header{"Idempotency-Key": {"controlled-target-mismatch"}})
	if status != 409 || n.outboundLookups.Load() != 1 || n.outboundEffects.Load() != 0 ||
		friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts`) != 0 {
		t.Fatal("ungranted native lookup target was pinned", status, string(data))
	}
	var result map[string]any
	if json.Unmarshal(data, &result) == nil && result["friend_outbound"] != nil {
		t.Fatal("ungranted native lookup target was disclosed")
	}
}

func TestOnboardingControlledPostgresLookupFailureRetainsOnlySafeDiagnostic(t *testing.T) {
	f, n, admin, ref, c, window := onboardingControlledFixture(t, "wsl")
	n.beforeOutboundLookup = func(context.Context, control.GroupBindingOutboundLookup) error {
		return bindingOutboundLookupError(&nativeauth.OnboardingLookupError{Stage: "search",
			Cause: &nativeauth.Error{Code: "native_business_rejected", Number: -1,
				Diagnostic: "provider_reason_unrecognized", Submitted: true}})
	}
	status, data, code := admin.requestRaw("POST", "/test-tools/onboarding-friend/challenges/"+c.ID+"/friend-target",
		control.OnboardingControlledTargetInput{Window: window.ID, Version: c.Version, Account: n.target.AddAccount},
		http.Header{"Idempotency-Key": {"controlled-safe-lookup-error"}})
	var result struct {
		Diagnostic control.GroupBindingLookupDiagnostic `json:"lookup_diagnostic"`
	}
	if status != 409 || code != "group_binding_outbound_rejected" || json.Unmarshal(data, &result) != nil ||
		result.Diagnostic.Platform != "wsl" || result.Diagnostic.Stage != "search" ||
		result.Diagnostic.Category != "upstream_rejected" || result.Diagnostic.ProviderCode != -1 ||
		result.Diagnostic.Reason != "provider_reason_unrecognized" || n.outboundLookups.Load() != 1 ||
		n.outboundEffects.Load() != 0 || friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts`) != 0 ||
		friendCount(t, f, `SELECT count(*) FROM test_windows WHERE id=$1 AND used=0`, window.ID) != 1 ||
		friendCount(t, f, `SELECT count(*) FROM audit_events WHERE resource_id=$1
			AND action='group_binding_outbound_lookup_failed'`, c.ID) != 1 {
		t.Fatal("controlled lookup failure lost safe diagnostics or consumed a mutation", status, code, string(data))
	}
	before := outboundCalls(n)
	if _, err := f.app.GroupBinding.Read(context.Background(), ref, c.ID); err != nil || outboundCalls(n) != before {
		t.Fatal("progress reading retried a controlled lookup", err)
	}
}

func TestOnboardingControlledPostgresWindowCannotAuthorizeCustomerChallenge(t *testing.T) {
	f, n, admin, _, _, window := onboardingControlledFixture(t, "wsl")
	c := f.createChallenge(t, f.alice, "wsl", "controlled-grant-not-customer")
	before := outboundCalls(n)
	for _, client := range []testClient{f.alice, admin} {
		status, _, _ := client.requestRaw("POST", "/test-tools/onboarding-friend/challenges/"+c.ID+"/friend-target",
			control.OnboardingControlledTargetInput{Window: window.ID, Version: c.Version, Account: n.target.AddAccount},
			http.Header{"Idempotency-Key": {"cannot-borrow-controlled-grant"}})
		if status != 403 && status != 404 {
			t.Fatal("a controlled window was usable for a customer challenge", status)
		}
	}
	_, err := f.app.GroupBinding.PrepareOutboundFriend(context.Background(), onboardingRef(t, f.alice, "wsl"), c.ID,
		control.GroupBindingFriendTargetInput{Version: c.Version, Account: n.target.AddAccount, Key: "direct-customer-no-grant"})
	friendFailure(t, err, 409, "group_binding_outbound_controlled_only")
	if outboundCalls(n) != before || friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts`) != 0 {
		t.Fatal("borrowed test authority reached a customer native lookup")
	}
}

func TestOnboardingControlledPostgresFinalGrantAndRoleFences(t *testing.T) {
	for _, change := range []string{"cancel", "deadline", "target", "action", "package", "binding", "binding_epoch",
		"isolated", "account_epoch", "admission", "native_role", "native_target"} {
		t.Run(change, func(t *testing.T) {
			f, n, admin, _, c, window := onboardingControlledFixture(t, "wsl")
			controlledFriendAPI(t, admin, c, window, "friend-target", "controlled-fence-prepare", n.target.AddAccount, 200)
			if change == "native_role" {
				n.beforeRoles = func() { n.robotRole = "member" }
			}
			n.beforeOutboundSend = func(ctx context.Context, request control.GroupBindingOutboundRequest) error {
				var query string
				id := window.ID
				switch change {
				case "cancel":
					query = `UPDATE test_windows SET cancelled=true WHERE id=$1`
				case "deadline":
					query = `UPDATE test_windows SET expires_at=clock_timestamp() WHERE id=$1`
				case "target":
					query = `UPDATE test_windows SET target_ids=ARRAY['other'] WHERE id=$1`
				case "action":
					query = `UPDATE test_windows SET actions=ARRAY['send_text'] WHERE id=$1`
				case "package":
					query = `UPDATE test_windows SET admission_mode='package' WHERE id=$1`
				case "binding":
					query, id = `UPDATE group_bindings SET active=false WHERE group_id=$1`, window.GroupID
				case "binding_epoch":
					query, id = `UPDATE group_bindings SET epoch=epoch+1 WHERE group_id=$1`, window.GroupID
				case "isolated":
					query, id = `UPDATE native_groups SET isolated_test=false WHERE id=$1`, window.GroupID
				case "account_epoch":
					query, id = `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, request.AccountID
				case "admission":
					query, id = `UPDATE accounts SET connection_enabled=false WHERE id=$1`, request.AccountID
				case "native_target":
					n.target.NativeID += "-changed"
					return nil
				}
				_, err := f.db.Platform.Exec(ctx, query, id)
				return err
			}
			out := controlledFriendAPI(t, admin, c, window, "friend-send", "controlled-fence-confirm", "", 200)
			if out.FriendOutbound == nil && change != "account_epoch" ||
				out.FriendOutbound != nil && out.FriendOutbound.Status != "failed" ||
				n.outboundEffects.Load() != 0 ||
				friendCount(t, f, `SELECT count(*) FROM test_windows WHERE id=$1 AND used=1`, window.ID) != 1 {
				t.Fatal("final grant change reached a native mutation or refunded its attempt", change, out)
			}
		})
	}
}

func TestOnboardingControlledPostgresUnknownReconcilesAfterCancellation(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, admin, ref, c, window := onboardingControlledFixture(t, platform)
			controlledFriendAPI(t, admin, c, window, "friend-target", "controlled-unknown-prepare", n.target.AddAccount, 200)
			n.outboundUnknown = true
			out := controlledFriendAPI(t, admin, c, window, "friend-send", "controlled-unknown-confirm", "", 200)
			if out.FriendOutbound == nil || out.FriendOutbound.Status != "unknown" || n.outboundEffects.Load() != 1 {
				t.Fatal("submitted uncertainty lost its durable record", out)
			}
			onboardingAPI(t, admin, "POST", "/test-windows/"+window.ID+"/cancel", "", map[string]any{}, 200)
			controlledFriendAPI(t, admin, c, window, "friend-send", "controlled-unknown-confirm", "", 409)
			n.outboundReconcileState = "friends"
			out, err := f.app.GroupBinding.ReconcileOutboundFriend(context.Background(), ref, c.ID,
				out.FriendOutbound.ID, control.GroupBindingConfirmation{Version: c.Version, Key: "controlled-read-only-reconcile"})
			if err != nil || out.FriendOutbound == nil || out.FriendOutbound.Status != "friends" ||
				out.IdentityProven || n.outboundEffects.Load() != 1 || n.outboundSends.Load() != 1 || n.outboundReconciles.Load() != 1 {
				t.Fatal("cancelled window prevented original readback or caused a replay", out, err)
			}
		})
	}
}
