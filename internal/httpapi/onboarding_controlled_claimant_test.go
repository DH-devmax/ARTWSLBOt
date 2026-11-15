package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"wangshangliao/internal/control"
)

// Keep account-specific role observations local to this synthetic fixture.
type onboardingClaimantNativeFixture struct {
	*bindingOutboundNativeFixture
	source, recipient, sourceNative string
	sourceRole, recipientRole       string
	recipientAbsent                 bool
}

func (n *onboardingClaimantNativeFixture) BindingRoles(ctx context.Context, in control.GroupBindingNativeScope) (control.GroupBindingRoleEvidence, error) {
	out, err := n.bindingOutboundNativeFixture.BindingRoles(ctx, in)
	if err != nil {
		return out, err
	}
	if in.NativeSenderID == n.sourceNative {
		out.SenderRole = n.sourceRole
	}
	switch in.AccountID {
	case n.source:
		out.RobotRole = n.sourceRole
	case n.recipient:
		out.RobotRole = n.recipientRole
		out.Present = out.Present && !n.recipientAbsent
	}
	return out, nil
}

type onboardingClaimantFixture struct {
	*controlledOnboardingFixture
	outbound  *onboardingClaimantNativeFixture
	ref       control.SessionRef
	scope     control.ControlledOnboardingScope
	challenge control.GroupBindingChallenge
	window    control.OnboardingControlledWindow
}

func newOnboardingClaimantFixture(t *testing.T, platform, role string) *onboardingClaimantFixture {
	t.Helper()
	return onboardingClaimantFixtureWithPriorProof(t, platform, role, true)
}

func onboardingClaimantFixtureWithPriorProof(t *testing.T, platform, role string, priorProof bool) *onboardingClaimantFixture {
	t.Helper()
	base, outbound, admin, ref := outboundFixture(t, platform)
	selection := &controlledOnboardingFixture{onboardingFixture: base, admin: admin, platform: platform,
		nativeClaimant: outbound.owner, nativeGroup: "synthetic-claimant-outbound-" + platform}
	if role == "admin" {
		selection.nativeClaimant = "synthetic-native-admin"
	}
	ctx := context.Background()
	if _, err := base.db.Platform.Exec(ctx, `UPDATE accounts SET testing_enabled=true,connection_enabled=true
		WHERE id=$1`, base.accounts[platform]); err != nil {
		t.Fatal(err)
	}
	// The existing controlled-scope fixture uses a chat claimant. This action
	// needs a distinct management account from its initial immutable INSERT.
	err := base.db.Platform.QueryRow(ctx, `INSERT INTO accounts(name,platform,workspace,purpose,native_uid,
		auth_state,credential_sealed,testing_enabled,connection_enabled)
		VALUES('Synthetic outbound claimant',$1,$1,'management',$2,'authenticated',$3,true,true)
		RETURNING id::text`, platform, selection.nativeClaimant,
		[]byte("synthetic-outbound-claimant-credential-"+platform)).Scan(&selection.claimant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = base.db.Platform.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until,observed_at,snapshot)
		VALUES($1,true,'synthetic-outbound-claimant-receiver',now()+interval '1 hour',now(),'{"state":"connected"}')`,
		selection.claimant); err != nil {
		t.Fatal(err)
	}
	if err = base.db.Platform.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name,isolated_test)
		VALUES($1,$2,'Synthetic claimant outbound group',true) RETURNING id::text`,
		platform, selection.nativeGroup).Scan(&selection.group); err != nil {
		t.Fatal(err)
	}
	if _, err = base.db.Platform.Exec(ctx, `INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
		VALUES($1,$3,'admin',clock_timestamp(),'native'),($2,$3,$4,clock_timestamp(),'native')`,
		base.accounts[platform], selection.claimant, selection.group, role); err != nil {
		t.Fatal(err)
	}
	var receiverNative string
	if err = base.db.Platform.QueryRow(ctx, `SELECT native_uid FROM accounts WHERE id=$1`,
		base.accounts[platform]).Scan(&receiverNative); err != nil {
		t.Fatal(err)
	}
	outbound.target = control.GroupBindingFriendTarget{NativeID: receiverNative,
		AddAccount: "synthetic-receiver-" + platform, Name: "Synthetic receiving robot"}
	native := &onboardingClaimantNativeFixture{bindingOutboundNativeFixture: outbound,
		source: selection.claimant, recipient: base.accounts[platform], sourceNative: selection.nativeClaimant,
		sourceRole: role, recipientRole: "admin"}
	base.app.GroupBinding.Native = native
	f := &onboardingClaimantFixture{controlledOnboardingFixture: selection, outbound: native, ref: ref}
	if priorProof {
		previous := selection.createScope(t, "claimant-prior-scope")
		created := selection.challenge(t, previous.ID, "claimant-prior-challenge")
		proven := base.prove(t, admin, platform, created, selection.nativeClaimant, selection.nativeGroup)
		raw := onboardingAPI(t, admin, http.MethodPost, "/group-binding/challenges/"+created.ID+"/confirm",
			"claimant-prior-confirm", map[string]int64{"version": proven.Version}, http.StatusOK)
		var confirmed control.GroupBindingChallenge
		if err = json.Unmarshal(raw, &confirmed); err != nil {
			t.Fatal(err)
		}
		if role == "admin" {
			if confirmed.State != "review" {
				t.Fatal("prior administrator proof bypassed review", confirmed)
			}
			raw = onboardingAPI(t, admin, http.MethodPost, "/group-binding/challenges/"+created.ID+"/review", "",
				map[string]any{"version": confirmed.Version, "approve": true}, http.StatusOK)
		}
		if field(t, raw, "state") != "bound" {
			t.Fatal("prior controlled ownership was not established")
		}
		onboardingAPI(t, admin, http.MethodPost, controlledOnboardingScopePath+"/"+previous.ID+"/close", "",
			map[string]int64{"version": previous.Version}, http.StatusOK)
	}
	f.scope = selection.createScope(t, "claimant-outbound-scope")
	if !priorProof {
		if _, err = base.db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id)
			VALUES($1,$2,$3)`, selection.group, f.scope.TenantID, native.recipient); err != nil {
			t.Fatal(err)
		}
	}
	f.challenge = selection.challenge(t, f.scope.ID, "claimant-outbound-parent")
	onboardingAPI(t, admin, http.MethodPut, "/system/group-binding/outbound-friend-gates/"+selection.claimant, "",
		control.GroupBindingFriendGateInput{Version: 0, State: "open", Evidence: []string{bindingFriendFixtureGateEvidence}},
		http.StatusOK)
	return f
}

func (f *onboardingClaimantFixture) windowInput() control.OnboardingControlledWindowInput {
	return control.OnboardingControlledWindowInput{ChallengeID: f.challenge.ID, Version: f.challenge.Version,
		GroupID: f.group, Account: f.outbound.target.AddAccount, TargetID: f.outbound.target.NativeID,
		Seconds: 300, Budget: 1, AsClaimant: true}
}

func (f *onboardingClaimantFixture) openWindow(t *testing.T) {
	t.Helper()
	raw := onboardingAPI(t, f.admin, http.MethodPost, "/test-tools/onboarding-friend/windows", "",
		f.windowInput(), http.StatusCreated)
	if err := json.Unmarshal(raw, &f.window); err != nil || f.window.ID == "" || f.window.Remaining != 1 {
		t.Fatal("claimant window lost its single persisted attempt", f.window, err)
	}
}

func (f *onboardingClaimantFixture) prepare(t *testing.T) control.GroupBindingChallenge {
	t.Helper()
	out := controlledFriendAPI(t, f.admin, f.challenge, f.window, "friend-target",
		"claimant-outbound-prepare", f.outbound.target.AddAccount, http.StatusOK)
	if out.FriendOutbound == nil {
		t.Fatal("claimant preparation omitted its durable operation", out)
	}
	return out
}

func (f *onboardingClaimantFixture) send(t *testing.T, key string) control.GroupBindingChallenge {
	t.Helper()
	return controlledFriendAPI(t, f.admin, f.challenge, f.window, "friend-send", key, "", http.StatusOK)
}

func (f *onboardingClaimantFixture) assertUnproven(t *testing.T, c control.GroupBindingChallenge) {
	t.Helper()
	if c.ID != f.challenge.ID || c.FriendFlow != "incoming_contact" || c.Mode != "operator_demo" ||
		c.Robot.ID != f.outbound.recipient || c.IdentityProven || c.CanConfirm || c.CanAcceptInvitation ||
		c.ProofKind != "controlled_test" || c.ControlledScope == nil || c.ControlledScope.ID != f.scope.ID {
		t.Fatal("outgoing sample changed its receiving parent or established proof", c)
	}
	if friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_challenges
		WHERE id=$1 AND account_id=$2 AND controlled_scope_id=$3 AND friend_flow='incoming_contact'
		AND identity_sealed IS NULL AND identity_event_id IS NULL AND identity_friend_proof_id IS NULL
		AND proof_sealed IS NULL AND proof_event_id IS NULL`,
		f.challenge.ID, f.outbound.recipient, f.scope.ID) != 1 ||
		friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_friend_proofs`) != 0 ||
		friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_ownership
			WHERE proof_challenge_id=$1`, f.challenge.ID) != 0 ||
		friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_bindings
			WHERE group_id=$1 AND (management_enabled OR ai_enabled)`, f.group) != 0 {
		t.Fatal("outgoing sample persisted identity, ownership, or runtime activation")
	}
}

func (f *onboardingClaimantFixture) assertUsage(t *testing.T, used int) {
	t.Helper()
	scope, err := f.app.GroupBinding.ReadControlledScope(context.Background(), f.ref, f.scope.ID)
	if err != nil || scope.NativeAttempts != used || scope.ModelCalls != 0 || scope.ReservedUSDT != "0.000000" {
		t.Fatal("claimant operation lost its shared scope usage", scope, err)
	}
	if friendCount(t, f.onboardingFixture, `SELECT count(*) FROM test_windows
		WHERE id=$1 AND used=$2 AND budget=1 AND account_ids=ARRAY[$3::uuid]`,
		f.window.ID, used, f.claimant) != 1 ||
		friendCount(t, f.onboardingFixture, `SELECT count(*) FROM controlled_onboarding_usage
			WHERE scope_id=$1 AND kind='native'`, f.scope.ID) != used {
		t.Fatal("claimant operation consumed the wrong account window or duplicated its reservation")
	}
}

func TestOnboardingControlledClaimantPostgresSelectedSourceSingleSend(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, role := range []string{"owner", "admin"} {
			t.Run(platform+"/"+role, func(t *testing.T) {
				f := newOnboardingClaimantFixture(t, platform, role)
				f.openWindow(t)
				var lookup control.GroupBindingOutboundLookup
				f.outbound.beforeOutboundLookup = func(_ context.Context, in control.GroupBindingOutboundLookup) error {
					f.outbound.outboundMu.Lock()
					lookup = in
					f.outbound.outboundMu.Unlock()
					return nil
				}
				prepared := f.prepare(t)
				f.outbound.outboundMu.Lock()
				selected := lookup
				f.outbound.outboundMu.Unlock()
				if selected.AccountID != f.claimant || selected.Platform != platform ||
					selected.AddAccount != f.outbound.target.AddAccount {
					t.Fatal("lookup used an unselected source or receiver", selected)
				}
				f.assertUnproven(t, prepared)
				f.assertUsage(t, 0)
				if prepared.FriendOutbound == nil || prepared.FriendOutbound.Status != "prepared" ||
					!prepared.FriendOutbound.CanSend || f.outbound.outboundLookups.Load() != 1 ||
					f.outbound.outboundSends.Load() != 0 || f.outbound.outboundEffects.Load() != 0 {
					t.Fatal("claimant preparation dispatched a mutation or lost its operation", prepared)
				}
				if friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts a
					JOIN group_binding_challenges c ON c.id=a.challenge_id
					JOIN onboarding_test_scopes s ON s.id=c.controlled_scope_id
					JOIN group_bindings b ON b.group_id=s.group_id
					WHERE a.id=$1 AND a.account_id=s.claimant_account_id AND a.account_id<>c.account_id
					AND a.account_epoch=s.claimant_epoch AND a.account_version=s.claimant_version
					AND c.account_id=s.account_id AND b.primary_account_id=c.account_id AND b.active
					AND a.test_window_id=$2 AND a.attempts_used=0`,
					prepared.FriendOutbound.ID, f.window.ID) != 1 {
					t.Fatal("ledger confused the selected claimant with its receiving primary account")
				}
				var sealed []byte
				if err := f.db.Platform.QueryRow(context.Background(), `SELECT payload_sealed
					FROM group_binding_outbound_attempts WHERE id=$1`, prepared.FriendOutbound.ID).Scan(&sealed); err != nil {
					t.Fatal(err)
				}
				raw, err := f.vault.Open("group-binding-outbound:"+prepared.FriendOutbound.ID, sealed)
				if err != nil {
					t.Fatal(err)
				}
				var payload struct {
					Request    control.GroupBindingOutboundRequest
					Controlled *struct {
						Challenge, Account, Window, Group string
						Epoch, AccountVersion             int64
						Claimant                          *struct {
							Scope, Account, Recipient    string
							ScopeVersion, Epoch, Version int64
							BindingVersion               int64
						}
					}
				}
				err = json.Unmarshal(raw, &payload)
				clear(raw)
				if err != nil || payload.Controlled == nil || payload.Controlled.Claimant == nil {
					t.Fatal("sealed request omitted its receiver or claimant snapshot", err)
				}
				auth, claimant := payload.Controlled, payload.Controlled.Claimant
				if auth.Challenge != f.challenge.ID || auth.Account != f.outbound.recipient ||
					auth.Window != f.window.ID || auth.Group != f.group ||
					claimant.Scope != f.scope.ID || claimant.ScopeVersion != f.scope.Version ||
					claimant.Account != f.claimant || claimant.Recipient != f.outbound.target.NativeID ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_bindings
						WHERE group_id=$1 AND version=$2`, f.group, claimant.BindingVersion) != 1 ||
					payload.Request.AccountID != claimant.Account || payload.Request.AccountEpoch != claimant.Epoch ||
					payload.Request.AccountVersion != claimant.Version ||
					payload.Request.Target.NativeID != claimant.Recipient ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_challenges
						WHERE id=$1 AND account_id=$2 AND account_epoch=$3 AND account_version=$4`,
						f.challenge.ID, auth.Account, auth.Epoch, auth.AccountVersion) != 1 {
					t.Fatal("sealed authorization replaced its receiving snapshot with the actual source")
				}
				sent := f.send(t, "claimant-outbound-confirm")
				f.assertUnproven(t, sent)
				f.assertUsage(t, 1)
				f.outbound.outboundMu.Lock()
				request := f.outbound.lastOutbound
				f.outbound.outboundMu.Unlock()
				if sent.FriendOutbound == nil || sent.FriendOutbound.ID != prepared.FriendOutbound.ID ||
					sent.FriendOutbound.Status != "submitted" || request.AccountID != f.claimant ||
					request.SelfID != f.nativeClaimant || request.Target.NativeID != f.outbound.target.NativeID ||
					request.Target.AddAccount != f.outbound.target.AddAccount ||
					request.NativeGroupID != "" || request.NativeSenderID != "" ||
					f.outbound.outboundSends.Load() != 1 || f.outbound.outboundEffects.Load() != 1 {
					t.Fatal("claimant send changed direction, target, or attempt count", sent, request)
				}
				before := outboundCalls(f.outbound.bindingOutboundNativeFixture)
				replay := f.send(t, "claimant-outbound-confirm")
				controlledFriendAPI(t, f.admin, f.challenge, f.window, "friend-send",
					"claimant-outbound-replacement", "", http.StatusConflict)
				if replay.FriendOutbound == nil || replay.FriendOutbound.ID != sent.FriendOutbound.ID ||
					outboundCalls(f.outbound.bindingOutboundNativeFixture) != before {
					t.Fatal("claimant replay dispatched another native operation", replay)
				}
				f.assertUsage(t, 1)
				f.assertUnproven(t, f.readChallenge(t, f.challenge))
				var retained []byte
				if err = f.db.Platform.QueryRow(context.Background(), `SELECT payload_sealed
					FROM group_binding_outbound_attempts WHERE id=$1`, prepared.FriendOutbound.ID).Scan(&retained); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(sealed, retained) ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts`) != 1 {
					t.Fatal("confirmation or replay rewrote or duplicated its sealed request")
				}
			})
		}
	}
}

func TestOnboardingControlledClaimantPostgresWindowAdmission(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingClaimantFixture(t, platform, "owner")
			for _, change := range []string{"target", "budget", "group", "member", "receiver-absent"} {
				t.Run(change, func(t *testing.T) {
					in := f.windowInput()
					code := "onboarding_controlled_target_changed"
					before := outboundCalls(f.outbound.bindingOutboundNativeFixture)
					switch change {
					case "target":
						in.TargetID = "synthetic-unselected-receiver"
					case "budget":
						in.Budget = 2
					case "group":
						in.GroupID = control.NewID()
						code = "controlled_onboarding_scope_inactive"
					case "member":
						f.outbound.sourceRole = "member"
						defer func() { f.outbound.sourceRole = "owner" }()
						code = "fresh_test_account_role_required"
					case "receiver-absent":
						f.outbound.recipientAbsent = true
						defer func() { f.outbound.recipientAbsent = false }()
						code = "fresh_test_account_role_required"
					}
					out, err := f.app.GroupBinding.CreateOnboardingControlledWindow(context.Background(), f.ref, in)
					friendFailure(t, err, http.StatusConflict, code)
					if out.ID != "" || f.outbound.outboundLookups.Load() != 0 ||
						f.outbound.outboundSends.Load() != 0 || f.outbound.outboundEffects.Load() != 0 ||
						friendCount(t, f.onboardingFixture, `SELECT count(*) FROM test_windows`) != 0 ||
						friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts`) != 0 ||
						friendCount(t, f.onboardingFixture, `SELECT count(*) FROM controlled_onboarding_usage
							WHERE scope_id=$1`, f.scope.ID) != 0 {
						t.Fatal("invalid claimant selection created authority, usage, or native work", out)
					}
					if change != "member" && change != "receiver-absent" &&
						outboundCalls(f.outbound.bindingOutboundNativeFixture) != before {
						t.Fatal("invalid persisted selection reached native role reads")
					}
				})
			}
			for _, field := range []string{"account_id", "source_account_id", "claimant_account_id"} {
				t.Run(field, func(t *testing.T) {
					raw, err := json.Marshal(f.windowInput())
					if err != nil {
						t.Fatal(err)
					}
					var in map[string]any
					if err = json.Unmarshal(raw, &in); err != nil {
						t.Fatal(err)
					}
					in[field] = f.outbound.recipient
					before := outboundCalls(f.outbound.bindingOutboundNativeFixture)
					onboardingAPI(t, f.admin, http.MethodPost, "/test-tools/onboarding-friend/windows", "",
						in, http.StatusBadRequest)
					if outboundCalls(f.outbound.bindingOutboundNativeFixture) != before ||
						friendCount(t, f.onboardingFixture, `SELECT count(*) FROM test_windows`) != 0 {
						t.Fatal("browser-selected source became a persisted grant")
					}
				})
			}
			f.assertUnproven(t, f.readChallenge(t, f.challenge))
		})
	}
}

func TestOnboardingControlledClaimantPostgresRequiresExistingScope(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, _, ref, c, legacy := onboardingControlledFixture(t, platform)
			before := outboundCalls(n)
			out, err := f.app.GroupBinding.CreateOnboardingControlledWindow(context.Background(), ref,
				control.OnboardingControlledWindowInput{ChallengeID: c.ID, Version: c.Version,
					GroupID: legacy.GroupID, Account: n.target.AddAccount, TargetID: n.target.NativeID,
					Seconds: 300, Budget: 1, AsClaimant: true})
			friendFailure(t, err, http.StatusConflict, "controlled_onboarding_scope_inactive")
			if out.ID != "" || outboundCalls(n) != before ||
				c.FriendFlow != "incoming_contact" || c.IdentityProven || c.ControlledScope != nil ||
				friendCount(t, f, `SELECT count(*) FROM test_windows
					WHERE id=$1 AND account_ids=ARRAY[$2::uuid] AND used=0`, legacy.ID, c.Robot.ID) != 1 ||
				friendCount(t, f, `SELECT count(*) FROM test_windows`) != 1 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts`) != 0 {
				t.Fatal("claimant mode borrowed an ordinary parent or changed the default window", out, c)
			}
		})
	}
}

func TestOnboardingControlledClaimantPostgresLookupPinsReceiver(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingClaimantFixture(t, platform, "owner")
			f.openWindow(t)
			before := outboundCalls(f.outbound.bindingOutboundNativeFixture)
			_, err := f.app.GroupBinding.PrepareControlledOutboundFriend(context.Background(), f.ref, f.challenge.ID,
				control.OnboardingControlledTargetInput{Window: f.window.ID, Version: f.challenge.Version,
					Account: "synthetic-other-add-account", Key: "claimant-wrong-lookup-selector"})
			friendFailure(t, err, http.StatusConflict, "onboarding_controlled_target_changed")
			if outboundCalls(f.outbound.bindingOutboundNativeFixture) != before {
				t.Fatal("unselected lookup account reached the native port")
			}
			f.outbound.outboundEvidence = func(e *control.GroupBindingOutboundEvidence) {
				e.Target.NativeID = "synthetic-other-native-receiver"
			}
			_, err = f.app.GroupBinding.PrepareControlledOutboundFriend(context.Background(), f.ref, f.challenge.ID,
				control.OnboardingControlledTargetInput{Window: f.window.ID, Version: f.challenge.Version,
					Account: f.outbound.target.AddAccount, Key: "claimant-wrong-resolved-receiver"})
			friendFailure(t, err, http.StatusConflict, "onboarding_controlled_target_changed")
			if f.outbound.outboundLookups.Load() != 1 ||
				f.outbound.outboundSends.Load() != 0 || f.outbound.outboundEffects.Load() != 0 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts`) != 0 {
				t.Fatal("lookup resolved an unselected receiver into a durable request", err)
			}
			f.assertUsage(t, 0)
			f.assertUnproven(t, f.readChallenge(t, f.challenge))
		})
	}
}

func TestOnboardingControlledClaimantPostgresFinalSubmissionFences(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, change := range []string{"source-epoch", "source-version", "receiver-epoch", "receiver-version",
			"window-cancel", "window-deadline", "binding-epoch", "binding-version", "binding-primary",
			"scope-version", "scope-close", "source-role", "receiver-absent"} {
			t.Run(platform+"/"+change, func(t *testing.T) {
				f := newOnboardingClaimantFixture(t, platform, "owner")
				f.openWindow(t)
				prepared := f.prepare(t)
				invalidated := false
				// This hook runs after reservation and both fresh role reads, but
				// before the native port enters the final submission transaction.
				f.outbound.beforeOutboundSend = func(ctx context.Context, _ control.GroupBindingOutboundRequest) error {
					query, id := "", f.claimant
					switch change {
					case "source-epoch":
						query = `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`
					case "source-version":
						query = `UPDATE accounts SET version=version+1 WHERE id=$1`
					case "receiver-epoch":
						query, id = `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, f.outbound.recipient
					case "receiver-version":
						query, id = `UPDATE accounts SET version=version+1 WHERE id=$1`, f.outbound.recipient
					case "window-cancel":
						query, id = `UPDATE test_windows SET cancelled=true WHERE id=$1`, f.window.ID
					case "window-deadline":
						query, id = `UPDATE test_windows SET expires_at=clock_timestamp() WHERE id=$1`, f.window.ID
					case "binding-epoch":
						query, id = `UPDATE group_bindings SET epoch=epoch+1 WHERE group_id=$1`, f.group
					case "binding-version":
						query, id = `UPDATE group_bindings SET version=version+1 WHERE group_id=$1`, f.group
					case "binding-primary":
						tag, err := f.db.Platform.Exec(ctx, `UPDATE group_bindings SET primary_account_id=$2
							WHERE group_id=$1`, f.group, f.claimant)
						invalidated = err == nil && tag.RowsAffected() == 1
						return err
					case "scope-version":
						query, id = `UPDATE onboarding_test_scopes SET version=version+1 WHERE id=$1`, f.scope.ID
					case "scope-close":
						closed, err := f.app.GroupBinding.CloseControlledScope(ctx, f.ref, f.scope.ID, f.scope.Version)
						invalidated = err == nil && closed.Version == f.scope.Version+1 && closed.State == "unknown"
						return err
					default:
						return errors.New("fresh role rejection should precede the send port")
					}
					tag, err := f.db.Platform.Exec(ctx, query, id)
					invalidated = err == nil && tag.RowsAffected() == 1
					if err == nil && !invalidated {
						return errors.New("synthetic final fence mutation did not affect its selected row")
					}
					return err
				}
				if change == "source-role" || change == "receiver-absent" {
					f.outbound.beforeRoles = func() {
						invalidated = true
						if change == "source-role" {
							f.outbound.sourceRole = "member"
						} else {
							f.outbound.recipientAbsent = true
						}
					}
				}
				_, err := f.app.GroupBinding.SendControlledOutboundFriend(context.Background(), f.ref, f.challenge.ID,
					control.OnboardingControlledConfirmation{Window: f.window.ID, Version: f.challenge.Version,
						Key: "claimant-invalidated-final-send"})
				if !invalidated || f.outbound.outboundEffects.Load() != 0 ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts
						WHERE id=$1 AND status='failed' AND attempts_used=1
						AND send_key='claimant-invalidated-final-send' AND account_id=$2`,
						prepared.FriendOutbound.ID, f.claimant) != 1 ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_challenges
						WHERE id=$1 AND identity_sealed IS NULL AND proof_sealed IS NULL`, f.challenge.ID) != 1 {
					t.Fatal("final claimant fence dispatched or released a changed authorization",
						change, "invalidated", invalidated, "effects", f.outbound.outboundEffects.Load(), err)
				}
				if change == "source-role" || change == "receiver-absent" {
					if f.outbound.outboundSends.Load() != 0 {
						t.Fatal("fresh role rejection reached the send port")
					}
				} else if f.outbound.outboundSends.Load() != 1 {
					t.Fatal("final fence test failed before reaching its native dispatch boundary")
				}
				f.assertUsage(t, 1)
			})
		}
	}
}

func TestOnboardingControlledClaimantPostgresClosedOrChangedBeforeSend(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, change := range []string{"closed", "scope-version"} {
			t.Run(platform+"/"+change, func(t *testing.T) {
				f := newOnboardingClaimantFixture(t, platform, "owner")
				f.openWindow(t)
				prepared := f.prepare(t)
				if change == "closed" {
					closed, err := f.app.GroupBinding.CloseControlledScope(context.Background(), f.ref, f.scope.ID, f.scope.Version)
					if err != nil || closed.State != "closed" ||
						friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts
							WHERE id=$1 AND status='failed' AND attempts_used=0 AND send_key IS NULL`,
							prepared.FriendOutbound.ID) != 1 {
						t.Fatal("closing an unsubmitted sample fabricated an attempt or unknown work", closed, err)
					}
				} else if _, err := f.db.Platform.Exec(context.Background(), `UPDATE onboarding_test_scopes
					SET version=version+1 WHERE id=$1`, f.scope.ID); err != nil {
					t.Fatal(err)
				}
				before := outboundCalls(f.outbound.bindingOutboundNativeFixture)
				_, err := f.app.GroupBinding.SendControlledOutboundFriend(context.Background(), f.ref, f.challenge.ID,
					control.OnboardingControlledConfirmation{Window: f.window.ID, Version: f.challenge.Version,
						Key: "claimant-no-send-after-scope-change"})
				var failure *control.Failure
				if !errors.As(err, &failure) || failure.Status != http.StatusConflict ||
					outboundCalls(f.outbound.bindingOutboundNativeFixture) != before ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts
						WHERE id=$1 AND attempts_used=0 AND send_key IS NULL`, prepared.FriendOutbound.ID) != 1 {
					t.Fatal("closed or replaced scope reached native work or consumed its unused reservation", err)
				}
				f.assertUsage(t, 0)
				f.assertUnproven(t, f.readChallenge(t, f.challenge))
			})
		}
	}
}

func TestOnboardingControlledClaimantPostgresPeerConnectionPinned(t *testing.T) {
	for _, change := range []string{"credential", "owner", "revision", "substituted-evidence"} {
		t.Run(change, func(t *testing.T) {
			f := newOnboardingClaimantFixture(t, "wsl", "owner")
			f.openWindow(t)
			prepared := f.prepare(t)
			f.outbound.beforeOutboundSend = func(ctx context.Context, in control.GroupBindingOutboundRequest) error {
				if in.ControlledPeer == nil || in.ControlledPeer.AccountID != f.outbound.recipient ||
					in.ControlledPeer.NativeID != f.outbound.target.NativeID ||
					in.ControlledPeer.Connection.CredentialDigest == ([32]byte{}) {
					t.Fatal("prepared request omitted the recipient's authenticated connection")
				}
				var query string
				switch change {
				case "credential":
					query = `UPDATE accounts SET credential_sealed=credential_sealed || decode('01','hex') WHERE id=$1`
				case "owner":
					query = `UPDATE account_receivers SET owner='synthetic-replacement-owner' WHERE account_id=$1`
				case "revision":
					query = `UPDATE account_receivers SET revision=revision+1 WHERE account_id=$1`
				case "substituted-evidence":
					f.outbound.outboundEvidence = func(e *control.GroupBindingOutboundEvidence) {
						e.Peer.Connection.CredentialDigest[0] ^= 1
					}
					return nil
				}
				tag, err := f.db.Platform.Exec(ctx, query, f.outbound.recipient)
				if err == nil && tag.RowsAffected() != 1 {
					t.Fatal("recipient connection fixture was not changed")
				}
				return err
			}
			_, _ = f.app.GroupBinding.SendControlledOutboundFriend(context.Background(), f.ref, f.challenge.ID,
				control.OnboardingControlledConfirmation{Window: f.window.ID, Version: f.challenge.Version,
					Key: "claimant-pinned-recipient-" + change})
			if f.outbound.outboundEffects.Load() != 0 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts
					WHERE id=$1 AND status='failed' AND attempts_used=1`, prepared.FriendOutbound.ID) != 1 {
				t.Fatal("changed recipient connection produced a native effect")
			}
			f.assertUsage(t, 1)
			f.assertUnproven(t, f.readChallenge(t, f.challenge))
		})
	}
}

func TestOnboardingControlledClaimantPostgresUnknownCloseReconcilesOriginal(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, result := range []string{"friends", "rejected"} {
			t.Run(platform+"/"+result, func(t *testing.T) {
				f := newOnboardingClaimantFixture(t, platform, "owner")
				f.openWindow(t)
				prepared := f.prepare(t)
				f.outbound.outboundUnknown = true
				const sendKey = "claimant-unknown-confirm"
				unknown := f.send(t, sendKey)
				if unknown.FriendOutbound == nil || unknown.FriendOutbound.ID != prepared.FriendOutbound.ID ||
					unknown.FriendOutbound.Status != "unknown" || !unknown.FriendOutbound.CanReconcile ||
					unknown.FriendOutbound.CanSend || unknown.CanCancel || f.outbound.outboundEffects.Load() != 1 {
					t.Fatal("ambiguous claimant send did not retain a single unresolved operation", unknown)
				}
				f.assertUnproven(t, unknown)
				f.assertUsage(t, 1)
				replayCalls := outboundCalls(f.outbound.bindingOutboundNativeFixture)
				replay := f.send(t, sendKey)
				controlledFriendAPI(t, f.admin, f.challenge, f.window, "friend-send",
					"claimant-unknown-new-confirm", "", http.StatusConflict)
				if replay.FriendOutbound == nil || replay.FriendOutbound.ID != unknown.FriendOutbound.ID ||
					replay.FriendOutbound.Status != "unknown" ||
					outboundCalls(f.outbound.bindingOutboundNativeFixture) != replayCalls {
					t.Fatal("unknown confirmation replay resubmitted its original request", replay)
				}
				_, err := f.app.GroupBinding.Cancel(context.Background(), f.ref, f.challenge.ID, f.challenge.Version)
				friendFailure(t, err, http.StatusConflict, "group_binding_outbound_operation_unresolved")
				ctx := context.Background()
				var sealed []byte
				operation := unknown.FriendOutbound.ID
				if err = f.db.Platform.QueryRow(ctx, `SELECT payload_sealed FROM group_binding_outbound_attempts
					WHERE id=$1`, operation).Scan(&sealed); err != nil {
					t.Fatal(err)
				}
				before := outboundCalls(f.outbound.bindingOutboundNativeFixture)
				closed := controlledOnboardingScope(t, onboardingAPI(t, f.admin, http.MethodPost,
					controlledOnboardingScopePath+"/"+f.scope.ID+"/close", "",
					map[string]int64{"version": f.scope.Version}, http.StatusOK))
				current := f.readChallenge(t, f.challenge)
				if closed.State != "unknown" || closed.Version != f.scope.Version+1 ||
					closed.BlockedReason != "group_binding_operation_unresolved" ||
					current.State != f.challenge.State || current.Version != f.challenge.Version+1 ||
					current.CanCancel || current.FriendOutbound == nil ||
					current.FriendOutbound.Status != "unknown" || !current.FriendOutbound.CanReconcile ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM onboarding_test_scopes
						WHERE id=$1 AND state='closed' AND NOT onboarding_test_scope_active(id)`, f.scope.ID) != 1 ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts
						WHERE id=$1 AND status='unknown' AND attempts_used=1 AND send_key=$2
						AND account_id=$3`, operation, sendKey, f.claimant) != 1 {
					t.Fatal("scope close cancelled or replaced a possibly submitted claimant operation", closed, current)
				}
				// Closing both execution gates must not remove read-only recovery.
				onboardingAPI(t, f.admin, http.MethodPut, "/system/group-binding/outbound-friend-gates/"+f.claimant, "",
					control.GroupBindingFriendGateInput{Version: 1, State: "closed"}, http.StatusOK)
				if _, err = f.db.Platform.Exec(ctx, `UPDATE test_windows SET cancelled=true WHERE id=$1`, f.window.ID); err != nil {
					t.Fatal(err)
				}
				for _, key := range []string{sendKey, "claimant-unknown-replacement"} {
					controlledFriendAPI(t, f.admin, current, f.window, "friend-send", key, "", http.StatusConflict)
				}
				if err = f.app.GroupBinding.RecoverOutboundFriends(ctx); err != nil {
					t.Fatal(err)
				}
				if outboundCalls(f.outbound.bindingOutboundNativeFixture) != before {
					t.Fatal("closing, replaying, or recovering unknown work executed a native operation")
				}
				reconcile := func(key string) control.GroupBindingChallenge {
					t.Helper()
					out, err := f.app.GroupBinding.ReconcileOutboundFriend(ctx, f.ref, f.challenge.ID, operation,
						control.GroupBindingConfirmation{Version: current.Version, Key: key})
					if err != nil || out.FriendOutbound == nil || out.FriendOutbound.ID != operation {
						t.Fatal("closed-scope reconciliation lost its original sealed request", out, err)
					}
					f.assertUnproven(t, out)
					return out
				}
				f.outbound.outboundReconcileState = "available"
				observed := reconcile("claimant-unknown-no-conclusion")
				if observed.FriendOutbound.Status != "unknown" || observed.CanCancel ||
					observed.ControlledScope.State != "unknown" {
					t.Fatal("an absent application was treated as cancellation of unknown work", observed)
				}
				f.outbound.outboundReconcileState = "rejected"
				f.outbound.outboundEvidence = func(e *control.GroupBindingOutboundEvidence) { e.ApplicationID = "" }
				observed = reconcile("claimant-unknown-no-application")
				if observed.FriendOutbound.Status != "unknown" || observed.CanCancel {
					t.Fatal("rejection without application evidence settled the unknown request", observed)
				}
				f.outbound.outboundEvidence = nil
				if result == "friends" {
					f.outbound.outboundReconcileState = "pending"
					observed = reconcile("claimant-unknown-original-pending")
					if observed.FriendOutbound.Status != "submitted" {
						t.Fatal("original application evidence did not reconcile its unknown submission", observed)
					}
				}
				f.outbound.outboundReconcileState = result
				observed = reconcile("claimant-unknown-original-result")
				want := "friends"
				if result == "rejected" {
					want = "failed"
				}
				var retained []byte
				if err = f.db.Platform.QueryRow(ctx, `SELECT payload_sealed FROM group_binding_outbound_attempts
					WHERE id=$1 AND attempts_used=1 AND send_key=$2 AND account_id=$3`,
					operation, sendKey, f.claimant).Scan(&retained); err != nil {
					t.Fatal(err)
				}
				wantReads := int32(3)
				if result == "friends" {
					wantReads++
				}
				if observed.FriendOutbound.Status != want || observed.FriendOutbound.CanSend ||
					observed.ControlledScope.State != "closed" || !bytes.Equal(sealed, retained) ||
					f.outbound.outboundSends.Load() != 1 || f.outbound.outboundEffects.Load() != 1 ||
					f.outbound.outboundReconciles.Load() != wantReads {
					t.Fatal("historical reconciliation retried, rewrote, or reactivated the original request", observed)
				}
				f.assertUsage(t, 1)
				if result == "rejected" {
					cancelled, err := f.app.GroupBinding.Cancel(ctx, f.ref, f.challenge.ID, current.Version)
					if err != nil || cancelled.State != "cancelled" ||
						f.outbound.outboundSends.Load() != 1 || f.outbound.outboundEffects.Load() != 1 ||
						friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts
							WHERE id=$1 AND status='failed' AND attempts_used=1 AND send_key=$2
							AND account_id=$3 AND payload_sealed=$4`, operation, sendKey, f.claimant, sealed) != 1 {
						t.Fatal("settled rejection could not cancel its parent without resending", cancelled, err)
					}
					f.assertUsage(t, 1)
				}
			})
		}
	}
}

func TestOnboardingControlledClaimantPostgresDBRejectsWrongDirection(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingClaimantFixture(t, platform, "owner")
			f.openWindow(t)
			ctx := context.Background()
			var other string
			if err := f.db.Platform.QueryRow(ctx, `INSERT INTO accounts(name,platform,workspace,purpose,native_uid,
				auth_state,credential_sealed,testing_enabled,connection_enabled)
				VALUES('Synthetic unselected sender',$1,$1,'management',$2,'authenticated',$3,true,true)
				RETURNING id::text`, platform, "synthetic-unselected-source-"+platform,
				[]byte("synthetic-unselected-source-credential")).Scan(&other); err != nil {
				t.Fatal(err)
			}
			before := outboundCalls(f.outbound.bindingOutboundNativeFixture)
			for _, change := range []string{"source", "receiver"} {
				t.Run(change, func(t *testing.T) {
					tx, err := f.db.Platform.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback(ctx)
					source := f.claimant
					var tag pgconn.CommandTag
					if change == "source" {
						source = other
						tag, err = tx.Exec(ctx, `UPDATE test_windows w SET account_ids=ARRAY[a.id],
							account_epochs=jsonb_build_object(a.id::text,a.epoch)
							FROM accounts a WHERE w.id=$1 AND a.id=$2`, f.window.ID, source)
					} else {
						tag, err = tx.Exec(ctx, `UPDATE test_windows SET target_ids[3]=$2 WHERE id=$1`,
							f.window.ID, "onboarding_target:synthetic-unselected-native-receiver")
					}
					if err != nil || tag.RowsAffected() != 1 {
						t.Fatal("synthetic forged window could not reach the outbound DB guard", err)
					}
					// All ordinary row constraints match the forged window. The
					// scope-to-source/receiver trigger must reject the INSERT.
					_, err = tx.Exec(ctx, `INSERT INTO group_binding_outbound_attempts(id,challenge_id,
						actor_id,tenant_id,platform,account_id,account_epoch,account_version,
						applicant_version,challenge_version,gate_version,payload_sealed,prepare_key,
						status,attempts_used,expires_at,test_window_id)
						SELECT $1,c.id,c.applicant_id,c.tenant_id,c.platform,a.id,a.epoch,a.version,
							c.applicant_version,c.version,0,$5,$6,'prepared',0,w.expires_at,w.id
						FROM group_binding_challenges c JOIN accounts a ON a.id=$3
						JOIN test_windows w ON w.id=$4 WHERE c.id=$2`,
						control.NewID(), f.challenge.ID, source, f.window.ID,
						bytes.Repeat([]byte{1}, 64), "claimant-forged-"+change)
					var failure *pgconn.PgError
					if !errors.As(err, &failure) || failure.Code != "23514" ||
						!strings.HasPrefix(failure.Message, "outbound friend ") {
						t.Fatal("forged direction escaped its scope trigger or failed for an unrelated reason", err)
					}
				})
			}
			if outboundCalls(f.outbound.bindingOutboundNativeFixture) != before ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts`) != 0 {
				t.Fatal("SQL-only forged direction produced a durable operation or native effect")
			}
			f.assertUsage(t, 0)
			f.assertUnproven(t, f.readChallenge(t, f.challenge))
		})
	}
}
