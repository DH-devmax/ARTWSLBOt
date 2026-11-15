package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"testing"

	"wangshangliao/internal/control"
)

// Never solve or attach a CAPTCHA in this suite, including during preparation.
func controlledCaptchaRequest(t *testing.T, client testClient, method, path, key string, body any, want int, extra ...http.Header) json.RawMessage {
	t.Helper()
	headers := []http.Header{{"Idempotency-Key": {key}}}
	headers = append(headers, extra...)
	status, raw, code := client.requestRaw(method, path, body, headers...)
	if status != want {
		t.Fatalf("%s %s: got %d (%s), want %d", method, path, status, code, want)
	}
	return raw
}

func controlledCaptchaDenied(t *testing.T, client testClient, path string, body any, wantCode string) {
	t.Helper()
	status, _, code := client.requestRaw(http.MethodPost, path, body,
		http.Header{"Idempotency-Key": {"synthetic-captcha-denied"}})
	if status < 400 || status >= 500 || wantCode != "" && code != wantCode {
		t.Fatalf("POST %s: got %d (%s), want a client rejection (%s)", path, status, code, wantCode)
	}
}

func controlledCaptchaRequired(t *testing.T, raw json.RawMessage, want bool) {
	t.Helper()
	var view struct {
		Required *bool `json:"human_verification_required"`
	}
	if err := json.Unmarshal(raw, &view); err != nil || view.Required == nil || *view.Required != want {
		t.Fatalf("missing or incorrect human_verification_required (want %t): %s, %v", want, raw, err)
	}
}

func controlledCaptchaScope(t *testing.T, f *controlledOnboardingFixture) control.ControlledOnboardingScope {
	t.Helper()
	raw := controlledCaptchaRequest(t, f.admin, http.MethodPost, controlledOnboardingScopePath,
		"synthetic-captcha-scope", f.input(), http.StatusCreated)
	controlledCaptchaRequired(t, raw, false)
	return controlledOnboardingScope(t, raw)
}

func controlledCaptchaChallenge(t *testing.T, f *controlledOnboardingFixture, scope string) control.GroupBindingChallenge {
	t.Helper()
	raw := controlledCaptchaRequest(t, f.admin, http.MethodPost, "/group-binding/challenges",
		"synthetic-captcha-challenge", map[string]any{"platform": f.platform, "controlled_scope_id": scope},
		http.StatusCreated)
	var out control.GroupBindingChallenge
	if err := json.Unmarshal(raw, &out); err != nil || out.ControlledScope == nil ||
		out.ControlledScope.ID != scope || out.Platform != f.platform ||
		out.Scope.ActorID != f.adminID || out.ProofKind != "controlled_test" ||
		out.IdentityProven || out.CanConfirm {
		t.Fatal("ticket-free creation lost its scope or supplied identity", out, err)
	}
	var view struct {
		Scope json.RawMessage `json:"controlled_scope"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	controlledCaptchaRequired(t, view.Scope, false)
	return out
}

func controlledCaptchaPrepare(t *testing.T, f *controlledOnboardingFixture, c control.GroupBindingChallenge) control.GroupBindingChallenge {
	t.Helper()
	controlledCaptchaRequest(t, f.admin, http.MethodPut,
		"/system/group-binding/friend-gates/"+f.accounts[f.platform], "",
		control.GroupBindingFriendGateInput{State: "open", Evidence: []string{bindingFriendFixtureGateEvidence}},
		http.StatusOK)
	raw := controlledCaptchaRequest(t, f.admin, http.MethodPost,
		"/group-binding/challenges/"+c.ID+"/friend-candidate", "synthetic-captcha-candidate",
		map[string]any{"version": c.Version, "nickname": "Synthetic applicant"}, http.StatusOK)
	if err := json.Unmarshal(raw, &c); err != nil || c.FriendContact == nil ||
		c.FriendContact.Status != "prepared" || c.IdentityProven || c.CanConfirm {
		t.Fatal("synthetic contact preparation lost its pending decision", c, err)
	}
	return c
}

type controlledCaptchaEffects struct {
	Native  [8]int32
	Records [8]int
}

func controlledCaptchaSnapshot(t *testing.T, f *controlledOnboardingFixture, n *bindingFriendNativeFixture) controlledCaptchaEffects {
	t.Helper()
	out := controlledCaptchaEffects{Native: [8]int32{n.roles.Load(), n.invites.Load(), n.sends.Load(),
		n.lookups.Load(), n.approvals.Load(), n.rejections.Load(), n.submits.Load(), n.reconciles.Load()}}
	err := f.db.Platform.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM group_binding_challenges),
		(SELECT count(*) FROM group_binding_friend_attempts),
		(SELECT count(*) FROM group_binding_friend_proofs),
		(SELECT count(*) FROM group_bindings),
		(SELECT count(*) FROM group_claims),
		(SELECT count(*) FROM group_binding_ownership),
		(SELECT count(*) FROM controlled_onboarding_usage),
		(SELECT coalesce(sum(attempts_used),0) FROM group_binding_friend_attempts)`).
		Scan(&out.Records[0], &out.Records[1], &out.Records[2], &out.Records[3],
			&out.Records[4], &out.Records[5], &out.Records[6], &out.Records[7])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestControlledOnboardingCaptchaPostgresLiveScopeAndHTTPGuards(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, native := newControlledOnboardingContactFixture(t, platform)
			n := native.bindingFriendNativeFixture
			scope := controlledCaptchaScope(t, f)
			before := controlledCaptchaSnapshot(t, f, n)
			c := controlledCaptchaChallenge(t, f, scope.ID)
			after := controlledCaptchaSnapshot(t, f, n)
			want := before
			want.Records[0]++
			if after != want {
				t.Fatal("opening a controlled challenge performed extra work", before, after)
			}
			replay := controlledCaptchaChallenge(t, f, scope.ID)
			if replay.ID != c.ID || controlledCaptchaSnapshot(t, f, n) != after {
				t.Fatal("ticket-free create retry repeated work")
			}
			raw := controlledCaptchaRequest(t, f.admin, http.MethodGet,
				controlledOnboardingScopePath+"?platform="+platform, "", nil, http.StatusOK)
			var list struct {
				Items []json.RawMessage `json:"items"`
			}
			if err := json.Unmarshal(raw, &list); err != nil || len(list.Items) != 1 {
				t.Fatal("scope list lost its verification hint", err)
			}
			controlledCaptchaRequired(t, list.Items[0], false)

			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			anonymous := testClient{base: f.admin.base, workspace: platform, client: &http.Client{Jar: jar}}
			for _, route := range []struct {
				path string
				body any
			}{
				{"/group-binding/challenges", map[string]any{"platform": platform, "controlled_scope_id": scope.ID}},
				{"/group-binding/challenges/" + c.ID + "/friend-approve", map[string]any{"version": c.Version, "operation_id": control.NewID()}},
				{"/group-binding/challenges/" + c.ID + "/friend-reject", map[string]any{"version": c.Version, "operation_id": control.NewID()}},
			} {
				controlledCaptchaDenied(t, anonymous, route.path, route.body, "authentication_required")
				for _, header := range []http.Header{
					{"X-CSRF-Token": {""}},
					{"Origin": {"https://untrusted.example"}},
				} {
					controlledCaptchaRequest(t, f.admin, http.MethodPost, route.path,
						"synthetic-captcha-http-guard", route.body, http.StatusForbidden, header)
				}
			}
			if controlledCaptchaSnapshot(t, f, n) != after {
				t.Fatal("progress or rejected HTTP guards performed native or authorization work")
			}
		})
	}
}

func TestControlledOnboardingCaptchaPostgresOrdinaryAndForgedRequests(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, native := newControlledOnboardingContactFixture(t, platform)
			n := native.bindingFriendNativeFixture
			scope := controlledCaptchaScope(t, f)
			for _, client := range []testClient{f.admin, f.alice} {
				client.workspace = platform
				// Seed an ordinary record through control without obtaining a CAPTCHA ticket.
				c, err := f.app.GroupBinding.Create(context.Background(), onboardingRef(t, client, platform),
					control.GroupBindingCreate{Platform: platform, Key: "synthetic-ordinary-challenge"})
				if err != nil {
					t.Fatal(err)
				}
				before := controlledCaptchaSnapshot(t, f, n)
				controlledCaptchaDenied(t, client, "/group-binding/challenges",
					map[string]any{"platform": platform}, "captcha_required")
				if client.client != f.admin.client {
					controlledCaptchaDenied(t, client, "/group-binding/challenges",
						map[string]any{"platform": platform, "controlled_scope_id": scope.ID}, "captcha_required")
				}
				for _, suffix := range []string{"/friend-approve", "/friend-reject"} {
					path := "/group-binding/challenges/" + c.ID + suffix
					body := map[string]any{"version": c.Version, "operation_id": control.NewID()}
					controlledCaptchaDenied(t, client, path, body, "captcha_required")
					body["controlled_scope_id"] = scope.ID
					controlledCaptchaRequest(t, client, http.MethodPost, path,
						"synthetic-forged-contact-scope", body, http.StatusBadRequest)
				}
				if controlledCaptchaSnapshot(t, f, n) != before {
					t.Fatal("ordinary request borrowed the active scope or changed authorization")
				}
			}
			before := controlledCaptchaSnapshot(t, f, n)
			for _, id := range []string{"not-an-id", control.NewID()} {
				controlledCaptchaDenied(t, f.admin, "/group-binding/challenges",
					map[string]any{"platform": platform, "controlled_scope_id": id}, "")
			}
			for name, value := range map[string]any{
				"human_verification_required": false,
				"proof_kind":                  "controlled_test",
				"controlled_scope":            map[string]any{"id": scope.ID, "human_verification_required": false},
			} {
				controlledCaptchaRequest(t, f.admin, http.MethodPost, "/group-binding/challenges",
					"synthetic-forged-exemption", map[string]any{"platform": platform, name: value},
					http.StatusBadRequest)
			}
			for _, extra := range []map[string]any{
				{"target_tenant_id": f.tenantA},
				{"restore_group_id": f.group, "binding_version": 1},
			} {
				extra["platform"], extra["controlled_scope_id"] = platform, scope.ID
				controlledCaptchaDenied(t, f.admin, "/group-binding/challenges", extra,
					"controlled_onboarding_own_workspace_required")
			}
			other := controlledOnboardingOtherAdmin(t, f)
			controlledCaptchaDenied(t, other, "/group-binding/challenges",
				map[string]any{"platform": platform, "controlled_scope_id": scope.ID}, "")
			wrong := f.admin
			wrong.workspace = "wsl"
			if platform == "wsl" {
				wrong.workspace = "lajiaoliao"
			}
			controlledCaptchaDenied(t, wrong, "/group-binding/challenges",
				map[string]any{"platform": wrong.workspace, "controlled_scope_id": scope.ID}, "")
			if controlledCaptchaSnapshot(t, f, n) != before {
				t.Fatal("forged fields, identities or platforms performed work")
			}
		})
	}
}

func TestControlledOnboardingCaptchaPostgresContactDecisionOnce(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, reject := range []bool{false, true} {
			action := "friend-approve"
			if reject {
				action = "friend-reject"
			}
			t.Run(platform+"/"+action, func(t *testing.T) {
				f, native := newControlledOnboardingContactFixture(t, platform)
				n := native.bindingFriendNativeFixture
				n.expiredContact = reject
				scope := controlledCaptchaScope(t, f)
				c := controlledCaptchaPrepare(t, f, controlledCaptchaChallenge(t, f, scope.ID))
				if c.FriendContact.CanReject != reject || c.FriendContact.CanApprove == reject ||
					n.submits.Load() != 0 {
					t.Fatal("preparation submitted or selected the wrong friend decision")
				}
				path := "/group-binding/challenges/" + c.ID + "/" + action
				body := map[string]any{"version": c.Version, "operation_id": c.FriendContact.ID}
				before := controlledCaptchaSnapshot(t, f, n)
				other := controlledOnboardingOtherAdmin(t, f)
				controlledCaptchaDenied(t, other, path, body, "")
				wrong := f.admin
				wrong.workspace = "wsl"
				if platform == "wsl" {
					wrong.workspace = "lajiaoliao"
				}
				controlledCaptchaDenied(t, wrong, path, body, "")
				forged := map[string]any{"version": c.Version, "operation_id": control.NewID()}
				controlledCaptchaDenied(t, f.admin, path, forged, "")
				if controlledCaptchaSnapshot(t, f, n) != before {
					t.Fatal("another actor, platform or operation consumed the prepared decision")
				}
				raw := controlledCaptchaRequest(t, f.admin, http.MethodPost, path,
					"synthetic-captcha-contact-once", body, http.StatusOK)
				var out control.GroupBindingChallenge
				if err := json.Unmarshal(raw, &out); err != nil || out.FriendContact == nil ||
					out.FriendContact.ID != c.FriendContact.ID || out.FriendContact.Status != "succeeded" ||
					out.IdentityProven || out.CanConfirm || n.submits.Load() != 1 {
					t.Fatal("ticket-free friend decision failed or supplied ownership", out, err)
				}
				if reject && (out.State != "cancelled" || out.BlockedReason != "group_binding_friend_reapply_required" ||
					n.rejections.Load() != 1 || n.approvals.Load() != 0) ||
					!reject && (n.approvals.Load() != 1 || n.rejections.Load() != 0) {
					t.Fatal("ticket-free confirmation performed the wrong native decision")
				}
				after := controlledCaptchaSnapshot(t, f, n)
				controlledCaptchaRequest(t, f.admin, http.MethodPost, path,
					"synthetic-captcha-contact-once", body, http.StatusOK)
				if controlledCaptchaSnapshot(t, f, n) != after ||
					after.Records[2] != 0 || after.Records[3] != 0 || after.Records[4] != 0 ||
					after.Records[5] != 0 || after.Records[6] != 1 || after.Records[7] != 1 {
					t.Fatal("friend decision replay repeated an effect or created identity/binding")
				}
			})
		}
	}
}

func TestControlledOnboardingCaptchaPostgresScopeFences(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, change := range []string{"closed_approve", "closed_reject", "expired", "blocked", "unknown", "preparing",
			"robot_epoch", "robot_version", "claimant_epoch", "claimant_version",
			"robot_testing_disabled", "claimant_testing_disabled"} {
			t.Run(platform+"/"+change, func(t *testing.T) {
				f, native := newControlledOnboardingContactFixture(t, platform)
				n := native.bindingFriendNativeFixture
				n.expiredContact = change == "closed_reject"
				scope := controlledCaptchaScope(t, f)
				c := controlledCaptchaPrepare(t, f, controlledCaptchaChallenge(t, f, scope.ID))
				ctx := context.Background()
				switch change {
				case "closed_approve", "closed_reject":
					raw := controlledCaptchaRequest(t, f.admin, http.MethodPost,
						controlledOnboardingScopePath+"/"+scope.ID+"/close", "",
						map[string]any{"version": scope.Version}, http.StatusOK)
					controlledCaptchaRequired(t, raw, true)
				case "expired", "unknown", "preparing", "blocked":
					state := change
					if change == "blocked" {
						state = "active"
					}
					// Only the synthetic lifecycle state changes; immutable deadlines and triggers remain intact.
					_, err := f.db.Platform.Exec(ctx, `UPDATE onboarding_test_scopes
						SET state=$2,blocked_reason='synthetic_scope_blocked',version=version+1 WHERE id=$1`,
						scope.ID, state)
					if err != nil {
						t.Fatal(err)
					}
				default:
					account := f.accounts[platform]
					if change == "claimant_epoch" || change == "claimant_version" || change == "claimant_testing_disabled" {
						account = f.claimant
					}
					query := `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`
					if change == "robot_version" || change == "claimant_version" {
						query = `UPDATE accounts SET version=version+1 WHERE id=$1`
					} else if change == "robot_testing_disabled" || change == "claimant_testing_disabled" {
						query = `UPDATE accounts SET testing_enabled=false WHERE id=$1`
					}
					if _, err := f.db.Platform.Exec(ctx, query, account); err != nil {
						t.Fatal(err)
					}
				}
				before := controlledCaptchaSnapshot(t, f, n)
				raw := controlledCaptchaRequest(t, f.admin, http.MethodGet,
					controlledOnboardingScopePath+"/"+scope.ID, "", nil, http.StatusOK)
				controlledCaptchaRequired(t, raw, true)
				controlledCaptchaDenied(t, f.admin, "/group-binding/challenges",
					map[string]any{"platform": platform, "controlled_scope_id": scope.ID}, "")
				for _, action := range []string{"friend-approve", "friend-reject"} {
					controlledCaptchaDenied(t, f.admin, "/group-binding/challenges/"+c.ID+"/"+action,
						map[string]any{"version": c.Version, "operation_id": c.FriendContact.ID}, "")
				}
				if controlledCaptchaSnapshot(t, f, n) != before {
					t.Fatal("stale controlled exemption reached a native port, reservation or binding")
				}
			})
		}
	}
}
