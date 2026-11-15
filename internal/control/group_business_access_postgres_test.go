package control

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"wangshangliao/internal/assistant"
	"wangshangliao/internal/features"
)

// These fixtures inherit the serial wsl_test-only reset guard. No model runner,
// native reader or native executor participates in this synthetic regression.
func newGroupBusinessAccessFixture(t *testing.T, platform string) *assistantBusinessFixture {
	t.Helper()
	f := newAssistantOutcomesFixture(t, platform)
	var name string
	if err := f.f.db.Platform.QueryRow(f.f.ctx, `SELECT current_database()`).Scan(&name); err != nil || name != "wsl_test" {
		t.Fatal("group access fixtures require wsl_test", err)
	}
	var executions, reads atomic.Int32
	f.assistant.ActionExecutor = func(context.Context, SessionRef, AssistantActionPlan) (SettingOperation, error) {
		executions.Add(1)
		return SettingOperation{}, Denied(503, "synthetic_unexpected_native_execution")
	}
	f.assistant.NativeMembers = func(context.Context, string, string, string, string) ([]AssistantMember, error) {
		reads.Add(1)
		return nil, Denied(503, "synthetic_unexpected_native_read")
	}
	f.assistant.NativeMessages = func(context.Context, string, string, string, string) ([]AssistantMessage, error) {
		reads.Add(1)
		return nil, Denied(503, "synthetic_unexpected_native_read")
	}
	t.Cleanup(func() {
		if executions.Load() != 0 || reads.Load() != 0 || f.f.attempts.Load() != 0 ||
			f.f.writes.Load() != 0 || f.f.roles.Load() != 0 || f.f.reads.Load() != 0 {
			t.Error("group access checks invoked native work")
		}
		for _, table := range []string{"native_setting_operations", "assistant_usage", "ai_calls", "test_windows"} {
			if n := f.f.count(`SELECT count(*) FROM ` + table); n != 0 {
				t.Errorf("group access checks created %d rows in %s", n, table)
			}
		}
	})
	return f
}

func groupBusinessAccessOperator(t *testing.T, f *assistantBusinessFixture, marker byte) (Actor, SessionRef) {
	t.Helper()
	actor := Actor{ID: NewID(), Role: "superadmin"}
	f.f.exec(`INSERT INTO users(id,username,password_hash,role)
		VALUES($1,$1::uuid::text,'synthetic-unused','superadmin')`, actor.ID)
	ref := SessionRef{hash: bytes.Repeat([]byte{marker}, 32), platform: f.scope.Platform}
	f.f.exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at)
		VALUES($1,$2,$3,clock_timestamp()+interval '1 hour')`,
		ref.hash, bytes.Repeat([]byte{marker + 1}, 32), actor.ID)
	return actor, ref
}

func groupBusinessAccessClaim(t *testing.T, f *assistantBusinessFixture, actor Actor, expired bool) string {
	t.Helper()
	claim := NewID()
	// Claim deadlines are immutable. Backdate an expired fixture on insertion
	// while retaining the schema's maximum ten-minute verification lifetime.
	f.f.exec(`INSERT INTO operator_demo_group_claims(id,actor_id,actor_version,tenant_id,group_id,platform,
		account_id,account_epoch,account_version,binding_epoch,binding_version,create_key,state,robot_role,
		owner_identity_hash,created_at,expires_at)
		SELECT $1,u.id,u.access_version,b.tenant_id,b.group_id,a.platform,
		a.id,a.epoch,a.version,b.epoch,b.version,$1::uuid::text,'active','admin',$4,
		clock_timestamp()-CASE WHEN $5 THEN interval '11 minutes' ELSE interval '0' END,
		clock_timestamp()+CASE WHEN $5 THEN interval '-1 minute' ELSE interval '10 minutes' END
		FROM group_bindings b JOIN accounts a ON a.id=b.primary_account_id JOIN users u ON u.id=$2
		WHERE b.group_id=$3`, claim, actor.ID, f.scope.GroupID,
		bindingIdentityDigest(f.scope.Platform, f.scope.GroupID, "native-owner"), expired)
	return claim
}

func groupBusinessAccessSetOwnership(t *testing.T, f *assistantBusinessFixture, state string) {
	t.Helper()
	switch state {
	case "unverified":
		f.f.exec(`DELETE FROM group_binding_ownership WHERE group_id=$1`, f.scope.GroupID)
	case "paused":
		f.f.exec(`UPDATE group_binding_ownership SET state='paused',version=version+1 WHERE group_id=$1`, f.scope.GroupID)
	default:
		t.Fatal("unsupported synthetic ownership state", state)
	}
}

func requireGroupBusinessAccess(t *testing.T, f *assistantBusinessFixture, actor Actor, want HumanGroupAccess) {
	t.Helper()
	got, err := ResolveHumanGroupAccess(f.f.ctx, f.f.db.Platform, actor, f.scope.Platform, f.scope.GroupID)
	if err != nil || got != want {
		t.Fatalf("group access = %+v, %v; want %+v", got, err, want)
	}
	err = RequireHumanGroupOwnership(f.f.ctx, f.f.db.Platform, actor, f.scope.Platform, f.scope.GroupID)
	if want.AccessMode == "manage" {
		if err != nil {
			t.Fatal("current ownership was not admitted", err)
		}
		return
	}
	code := "group_ownership_proof_required"
	if actor.Role == "superadmin" {
		code = "operator_demo_claim_required"
	}
	capabilityError(t, err, code)
}

func groupBusinessAccessContext(t *testing.T, f *assistantBusinessFixture, ref SessionRef, want HumanGroupAccess) {
	t.Helper()
	view, err := f.assistant.Context(f.f.ctx, ref, f.scope.GroupID, "", 0, 50)
	if err != nil || view.Scope == nil || *view.Scope != f.scope ||
		view.GroupAccess == nil || *view.GroupAccess != want || view.Total != 1 || view.NextOffset != -1 {
		t.Fatalf("incorrect selected context: %+v, %v", view, err)
	}
	var groups []assistantContextGroup
	if err = json.Unmarshal(view.Groups, &groups); err != nil || len(groups) != 1 ||
		groups[0].GroupID != f.scope.GroupID || groups[0].Platform != f.scope.Platform ||
		groups[0].PrimaryAccountID != f.scope.AccountID || groups[0].HumanGroupAccess != want {
		t.Fatal("directory and selected access disagree", string(view.Groups), err)
	}
	query, propose := false, false
	for _, capability := range view.Capabilities {
		if capability.ID == "blocked_words.query" {
			query = capability.Enabled
		}
		if capability.ID == "blocked_words.config_propose" {
			propose = capability.Enabled
		}
		if want.AccessMode == "read_only" && capability.Category != "query" &&
			(capability.Enabled || capability.BlockedReason != want.BlockedReason) {
			t.Fatalf("read-only context exposed a mutation capability: %+v", capability)
		}
	}
	if !query || propose != (want.AccessMode == "manage") || len(view.Actions) == 0 {
		t.Fatal("ownership did not distinguish queries from suggestions", query, propose)
	}
	if want.AccessMode == "read_only" {
		for _, action := range view.Actions {
			if action.Production || action.BlockReason != want.BlockedReason {
				t.Fatalf("read-only context admitted an action: %+v", action)
			}
		}
	}
	actor, err := f.assistant.actor(f.f.ctx, f.f.db.Platform, ref, false)
	if err != nil {
		t.Fatal(err)
	}
	tools, _, err := f.assistant.effectiveAssistantTools(f.f.ctx, actor, f.scope)
	if err != nil || !assistant.ContainsTool(tools, "get_group_config") {
		t.Fatal("current scope lost query tools", tools, err)
	}
	for _, name := range []string{"propose_group_config_change", "propose_group_action"} {
		if assistant.ContainsTool(tools, name) != (want.AccessMode == "manage") {
			t.Fatal("tool whitelist disagrees with ownership", name, tools)
		}
	}
}

type groupBusinessAccessBinding struct {
	Version, Epoch int64
	Config         string
	Management, AI bool
}

func groupBusinessAccessSnapshot(t *testing.T, f *assistantBusinessFixture) groupBusinessAccessBinding {
	t.Helper()
	var out groupBusinessAccessBinding
	if err := f.f.db.Platform.QueryRow(f.f.ctx, `SELECT version,epoch,config::text,management_enabled,ai_enabled
		FROM group_bindings WHERE group_id=$1`, f.scope.GroupID).
		Scan(&out.Version, &out.Epoch, &out.Config, &out.Management, &out.AI); err != nil {
		t.Fatal(err)
	}
	return out
}

func groupBusinessAccessSave(t *testing.T, f *assistantBusinessFixture, actor Actor, entry string, version int64) (FeatureCommit, error) {
	t.Helper()
	tx, err := f.f.db.Platform.Begin(f.f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.f.ctx)
	changes := FeatureChanges{"blocked_words": {"words": json.RawMessage(`["synthetic-ownership-word"]`)}}
	var out FeatureCommit
	switch entry {
	case "fields", "assistant_fields":
		out, err = SaveFeaturesTx(f.f.ctx, tx, actor, f.assistant.Catalog, f.scope.GroupID, version, changes, entry == "assistant_fields")
	case "full_config":
		config := f.f.config
		config.BlockedWords = []string{"synthetic-ownership-word"}
		out, err = CommitGroupConfigTx(f.f.ctx, tx, actor, f.f.tenant, f.scope.GroupID, version,
			config, nil, nil, "group_config_updated", map[string]any{"synthetic": true})
	default:
		t.Fatal("unknown configuration entry", entry)
	}
	if err == nil {
		err = tx.Commit(f.f.ctx)
	}
	return out, err
}

func groupBusinessAccessRejectWrites(t *testing.T, f *assistantBusinessFixture, actor Actor, ref SessionRef, session AssistantSession, code string) {
	t.Helper()
	before := groupBusinessAccessSnapshot(t, f)
	plans := f.f.count(`SELECT count(*) FROM assistant_plans`)
	actions := f.f.count(`SELECT count(*) FROM assistant_action_plans`)
	executions := f.f.count(`SELECT count(*) FROM assistant_plan_executions`)
	audits := f.f.count(`SELECT count(*) FROM audit_events`)
	_, err := f.assistant.propose(f.f.ctx, ref, session, assistantOutcomesConfigCall(t, f))
	capabilityError(t, err, code)
	// Ownership must reject this implemented action before accepting any target.
	_, err = f.assistant.proposeAction(f.f.ctx, ref, session, "mute_member", "", "", 1)
	capabilityError(t, err, code)
	for _, entry := range []string{"fields", "assistant_fields", "full_config"} {
		_, err = groupBusinessAccessSave(t, f, actor, entry, before.Version)
		capabilityError(t, err, code)
	}
	if after := groupBusinessAccessSnapshot(t, f); after != before {
		t.Fatal("rejected ownership changed binding configuration", before, after)
	}
	if f.f.count(`SELECT count(*) FROM assistant_plans`) != plans ||
		f.f.count(`SELECT count(*) FROM assistant_action_plans`) != actions ||
		f.f.count(`SELECT count(*) FROM audit_events`) != audits ||
		f.f.count(`SELECT count(*) FROM assistant_plan_executions`) != executions {
		t.Fatal("rejected ownership persisted a proposal, commit or audit")
	}
}

func groupBusinessAccessHistory(t *testing.T, f *assistantBusinessFixture, ref SessionRef, session AssistantSession) {
	t.Helper()
	turn := NewID()
	input := "synthetic protected group history"
	sealed, err := f.assistant.Vault.Seal("assistant:input:"+turn, []byte(input))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := f.assistant.authorizationHash(f.f.ctx, ref, session.Scope)
	if err != nil {
		t.Fatal(err)
	}
	f.f.exec(`INSERT INTO assistant_turns(id,session_id,actor_id,tenant_id,domain,idempotency_key,
		input_hash,authorization_hash,input_sealed,status,deadline,finished_at,module_policy_version)
		VALUES($1,$2,$3,nullif($4,'')::uuid,$5,$1::uuid::text,$6,$7,$8,'succeeded',
		clock_timestamp()+interval '90 seconds',clock_timestamp(),
		(SELECT version FROM assistant_module_exposure WHERE platform=$5))`,
		turn, session.ID, session.Actor.ID, session.Actor.TenantID, f.scope.Platform, bytes.Repeat([]byte{83}, 32), hash, sealed)
	rows, err := f.assistant.Transcript(f.f.ctx, ref, session.ID)
	if err != nil || len(rows) != 1 || rows[0].ID != turn || rows[0].Input != input || rows[0].Unavailable {
		t.Fatal("synthetic history was not readable before revocation", rows, err)
	}
}

func groupBusinessAccessQuery(t *testing.T, f *assistantBusinessFixture, ref SessionRef, session AssistantSession) {
	t.Helper()
	result, err := f.assistant.queryTool(f.f.ctx, ref, session,
		assistantOutcomesCall(t, "get_group_config", map[string]string{"group_id": f.scope.GroupID}))
	if err != nil {
		t.Fatal("authorized configuration query failed", err)
	}
	data, ok := result.Data.(map[string]any)
	if !ok || data["group_id"] != f.scope.GroupID || data["account_id"] != f.scope.AccountID || data["values"] == nil {
		t.Fatal("query did not return the selected group's configuration", result)
	}
	values, ok := data["values"].(map[string]any)
	if !ok || values["blocked_words"] == nil {
		t.Fatal("authorized query omitted visible configuration fields", result)
	}
}

func TestPostgresHumanGroupBusinessAccessCustomerOwnership(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, state := range []string{"unverified", "paused"} {
			t.Run(platform+"/"+state, func(t *testing.T) {
				f := newGroupBusinessAccessFixture(t, platform)
				session, err := f.assistant.Session(f.f.ctx, f.ref, f.session, true)
				if err != nil {
					t.Fatal(err)
				}
				active := HumanGroupAccess{OwnershipState: "active", AccessMode: "manage"}
				requireGroupBusinessAccess(t, f, session.Actor, active)
				groupBusinessAccessContext(t, f, f.ref, active)
				groupBusinessAccessQuery(t, f, f.ref, session)
				groupBusinessAccessHistory(t, f, f.ref, session)
				groupBusinessAccessSetOwnership(t, f, state)
				reason := "group_ownership_proof_required"
				if state == "paused" {
					reason = "group_ownership_reproof_required"
				}
				requireGroupBusinessAccess(t, f, session.Actor, HumanGroupAccess{
					OwnershipState: state, AccessMode: "read_only", BlockedReason: reason,
				})
				view, err := f.assistant.Context(f.f.ctx, f.ref, "", "", 0, 50)
				if err != nil || view.Total != 0 || string(view.Groups) != "[]" || view.NextOffset != -1 {
					t.Fatal("customer directory exposed an unowned group", view, err)
				}
				_, err = f.assistant.Context(f.f.ctx, f.ref, f.scope.GroupID, "", 0, 50)
				capabilityError(t, err, "group_ownership_proof_required")
				sessions := f.f.count(`SELECT count(*) FROM assistant_sessions`)
				_, err = f.assistant.CreateSession(f.f.ctx, f.ref, f.scope)
				capabilityError(t, err, "group_ownership_proof_required")
				if f.f.count(`SELECT count(*) FROM assistant_sessions`) != sessions {
					t.Fatal("unowned scope created an assistant session")
				}
				_, err = f.assistant.Sessions(f.f.ctx, f.ref, f.scope)
				capabilityError(t, err, "group_ownership_proof_required")
				_, err = f.assistant.Session(f.f.ctx, f.ref, f.session, false)
				capabilityError(t, err, "group_ownership_proof_required")
				_, err = f.assistant.Transcript(f.f.ctx, f.ref, f.session)
				capabilityError(t, err, "group_ownership_proof_required")
				_, err = f.assistant.queryTool(f.f.ctx, f.ref, session,
					assistantOutcomesCall(t, "get_group_config", map[string]string{"group_id": f.scope.GroupID}))
				capabilityError(t, err, "group_ownership_proof_required")
				groupBusinessAccessRejectWrites(t, f, session.Actor, f.ref, session, "group_ownership_proof_required")
			})
		}
	}
}

func TestPostgresHumanGroupBusinessAccessOperatorReadOnly(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, state := range []string{"unverified", "paused"} {
			t.Run(platform+"/"+state, func(t *testing.T) {
				f := newGroupBusinessAccessFixture(t, platform)
				actor, ref := groupBusinessAccessOperator(t, f, 85)
				groupBusinessAccessSetOwnership(t, f, state)
				want := HumanGroupAccess{OwnershipState: state, AccessMode: "read_only", BlockedReason: "operator_demo_claim_required"}
				requireGroupBusinessAccess(t, f, actor, want)
				groupBusinessAccessContext(t, f, ref, want)
				session, err := f.assistant.CreateSession(f.f.ctx, ref, f.scope)
				if err != nil {
					t.Fatal("operator could not open a query-only session", err)
				}
				groupBusinessAccessHistory(t, f, ref, session)
				groupBusinessAccessQuery(t, f, ref, session)
				raw, err := f.assistant.Sessions(f.f.ctx, ref, f.scope)
				var history []assistantSessionSummary
				if err != nil || json.Unmarshal(raw, &history) != nil || len(history) != 1 || history[0].ID != session.ID {
					t.Fatal("operator lost query-only session history", string(raw), err)
				}
				groupBusinessAccessRejectWrites(t, f, actor, ref, session, "operator_demo_claim_required")
			})
		}
	}
}

func TestPostgresHumanGroupBusinessAccessActiveOwnership(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newGroupBusinessAccessFixture(t, platform)
			customer, err := f.assistant.Session(f.f.ctx, f.ref, f.session, true)
			if err != nil {
				t.Fatal(err)
			}
			operator, ref := groupBusinessAccessOperator(t, f, 87)
			for _, person := range []struct {
				actor Actor
				ref   SessionRef
			}{{customer.Actor, f.ref}, {operator, ref}} {
				active := HumanGroupAccess{OwnershipState: "active", AccessMode: "manage"}
				requireGroupBusinessAccess(t, f, person.actor, active)
				groupBusinessAccessContext(t, f, person.ref, active)
				for _, entry := range []string{"fields", "assistant_fields", "full_config"} {
					before := groupBusinessAccessSnapshot(t, f)
					saved, err := groupBusinessAccessSave(t, f, person.actor, entry, before.Version)
					after := groupBusinessAccessSnapshot(t, f)
					if err != nil || saved.Applied || saved.Version != before.Version+1 ||
						after.Version != saved.Version || after.Epoch != before.Epoch+1 {
						t.Fatal("active customer ownership did not admit shared configuration", entry, saved, after, err)
					}
				}
			}
			if f.f.count(`SELECT count(*) FROM operator_demo_group_claims`) != 0 {
				t.Fatal("active customer ownership created a demo claim")
			}
		})
	}
}

func TestPostgresHumanGroupBusinessAccessDemoClaimInvalidation(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, scenario := range []string{"other_operator", "expired", "account_epoch", "account_version",
			"binding_version", "binding_epoch", "actor_version", "cancelled"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				f := newGroupBusinessAccessFixture(t, platform)
				actor, ref := groupBusinessAccessOperator(t, f, 89)
				groupBusinessAccessSetOwnership(t, f, "unverified")
				claim := groupBusinessAccessClaim(t, f, actor, scenario == "expired")
				if scenario != "expired" {
					manage := HumanGroupAccess{OwnershipState: "unverified", AccessMode: "manage"}
					requireGroupBusinessAccess(t, f, actor, manage)
					groupBusinessAccessContext(t, f, ref, manage)
				}
				switch scenario {
				case "other_operator":
					actor, ref = groupBusinessAccessOperator(t, f, 91)
				case "account_epoch":
					f.f.exec(`UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, f.scope.AccountID)
				case "account_version":
					f.f.exec(`UPDATE accounts SET version=version+1 WHERE id=$1`, f.scope.AccountID)
				case "binding_version":
					f.f.exec(`UPDATE group_bindings SET version=version+1 WHERE group_id=$1`, f.scope.GroupID)
				case "binding_epoch":
					f.f.exec(`UPDATE group_bindings SET epoch=epoch+1 WHERE group_id=$1`, f.scope.GroupID)
				case "actor_version":
					f.f.exec(`UPDATE users SET access_version=access_version+1 WHERE id=$1`, actor.ID)
					// The access-version trigger revokes website sessions too.
					// A fresh synthetic login must still reject the old claim.
					f.f.exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at)
						VALUES($1,$2,$3,clock_timestamp()+interval '1 hour')`,
						ref.hash, bytes.Repeat([]byte{90}, 32), actor.ID)
				case "cancelled":
					f.f.exec(`UPDATE operator_demo_group_claims SET state='cancelled',version=version+1 WHERE id=$1`, claim)
				}
				want := HumanGroupAccess{OwnershipState: "unverified", AccessMode: "read_only", BlockedReason: "operator_demo_claim_required"}
				requireGroupBusinessAccess(t, f, actor, want)
				groupBusinessAccessContext(t, f, ref, want)
				session, err := f.assistant.CreateSession(f.f.ctx, ref, f.scope)
				if err != nil {
					t.Fatal("invalid demo claim blocked query-only access", err)
				}
				groupBusinessAccessQuery(t, f, ref, session)
				groupBusinessAccessRejectWrites(t, f, actor, ref, session, "operator_demo_claim_required")
				if f.f.count(`SELECT count(*) FROM group_binding_ownership`) != 0 ||
					f.f.count(`SELECT count(*) FROM operator_demo_group_claims`) != 1 {
					t.Fatal("invalid claim was renewed or became customer ownership")
				}
			})
		}
	}
}

func TestPostgresHumanGroupBusinessAccessOwnSaveKeepsOriginalDemoDeadline(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, entry := range []string{"fields", "assistant_fields", "full_config", "assistant_approval"} {
			t.Run(platform+"/"+entry, func(t *testing.T) {
				f := newGroupBusinessAccessFixture(t, platform)
				actor, ref := groupBusinessAccessOperator(t, f, 93)
				groupBusinessAccessSetOwnership(t, f, "unverified")
				claim := groupBusinessAccessClaim(t, f, actor, false)
				manage := HumanGroupAccess{OwnershipState: "unverified", AccessMode: "manage"}
				requireGroupBusinessAccess(t, f, actor, manage)
				groupBusinessAccessContext(t, f, ref, manage)
				session, err := f.assistant.CreateSession(f.f.ctx, ref, f.scope)
				if err != nil {
					t.Fatal(err)
				}
				result, err := f.assistant.propose(f.f.ctx, ref, session, assistantOutcomesConfigCall(t, f))
				plan, ok := result.Data.(AssistantPlan)
				if err != nil || !ok || plan.Status != "pending_approval" || plan.ExecutionMode != "operator_demo" {
					t.Fatal("own current claim could not propose a configuration", result, err)
				}
				customer := Actor{ID: f.f.actor, TenantID: f.f.tenant, Role: "user"}
				requireGroupBusinessAccess(t, f, customer, HumanGroupAccess{
					OwnershipState: "unverified", AccessMode: "read_only", BlockedReason: "group_ownership_proof_required",
				})
				var claimBefore string
				if err = f.f.db.Platform.QueryRow(f.f.ctx, `SELECT (to_jsonb(c)-'state'-'version'-'blocked_reason')::text
					FROM operator_demo_group_claims c WHERE id=$1`, claim).Scan(&claimBefore); err != nil {
					t.Fatal(err)
				}
				before := groupBusinessAccessSnapshot(t, f)
				var saved FeatureCommit
				executions := 0
				word := "synthetic-ownership-word"
				if entry == "assistant_approval" {
					committed, approveErr := f.assistant.Approve(f.f.ctx, ref, plan.ID, plan.Digest, plan.Version)
					err = approveErr
					if err != nil || committed.Status != "committed" {
						t.Fatal("own current claim could not confirm a configuration", committed, err)
					}
					saved = FeatureCommit{Version: committed.CommittedVersion, Applied: committed.Applied}
					executions, word = 1, "synthetic-outcome-word"
				} else {
					saved, err = groupBusinessAccessSave(t, f, actor, entry, before.Version)
				}
				after := groupBusinessAccessSnapshot(t, f)
				var config features.Config
				if err != nil || saved.Applied || saved.Version != before.Version+1 ||
					after.Version != saved.Version || after.Epoch != before.Epoch+1 ||
					json.Unmarshal([]byte(after.Config), &config) != nil ||
					len(config.BlockedWords) != 1 || config.BlockedWords[0] != word {
					t.Fatal("verified demo configuration was not saved exactly once", saved, after, err)
				}
				var claimAfter string
				if err = f.f.db.Platform.QueryRow(f.f.ctx, `SELECT (to_jsonb(c)-'state'-'version'-'blocked_reason')::text
					FROM operator_demo_group_claims c WHERE id=$1`, claim).Scan(&claimAfter); err != nil {
					t.Fatal(err)
				}
				if claimBefore != claimAfter || f.f.count(`SELECT count(*) FROM operator_demo_group_claims`) != 2 ||
					f.f.count(`SELECT count(*) FROM group_binding_ownership`) != 0 {
					t.Fatal("configuration save altered immutable evidence or manufactured ownership")
				}
				var continued bool
				err = f.f.db.Platform.QueryRow(f.f.ctx, `SELECT old.state='cancelled' AND current.state='active'
					AND current.expires_at=old.expires_at AND current.created_at>=old.created_at
					AND current.actor_id=old.actor_id AND current.actor_version=old.actor_version
					AND current.tenant_id=old.tenant_id AND current.group_id=old.group_id
					AND current.platform=old.platform AND current.account_id=old.account_id
					AND current.account_version=old.account_version AND current.account_epoch=old.account_epoch
					AND current.robot_role=old.robot_role AND current.owner_identity_hash=old.owner_identity_hash
					AND current.binding_version=old.binding_version+1 AND current.binding_epoch=old.binding_epoch+1
					FROM operator_demo_group_claims old JOIN operator_demo_group_claims current
					ON current.actor_id=old.actor_id AND current.group_id=old.group_id AND current.id<>old.id
					WHERE old.id=$1`, claim).Scan(&continued)
				if err != nil || !continued {
					t.Fatal("own configuration did not continue the exact claim until its original deadline", continued, err)
				}
				requireGroupBusinessAccess(t, f, actor, manage)
				groupBusinessAccessContext(t, f, ref, manage)
				groupBusinessAccessQuery(t, f, ref, session)
				again, err := f.assistant.Approve(f.f.ctx, ref, plan.ID, plan.Digest, plan.Version)
				if entry == "assistant_approval" {
					if err != nil || again.ExecutionID == "" || again.CommittedVersion != saved.Version {
						t.Fatal("confirmed configuration did not reconcile its original execution", again, err)
					}
				} else {
					capabilityError(t, err, "assistant_scope_changed")
				}
				if groupBusinessAccessSnapshot(t, f) != after ||
					f.f.count(`SELECT count(*) FROM assistant_plan_executions`) != executions {
					t.Fatal("old configuration plan executed after the binding version changed")
				}
				restored, err := groupBusinessAccessSave(t, f, actor, "fields", saved.Version)
				if err != nil || restored.Version != saved.Version+1 ||
					f.f.count(`SELECT count(*) FROM operator_demo_group_claims WHERE state='active'`) != 1 ||
					f.f.count(`SELECT count(*) FROM operator_demo_group_claims WHERE expires_at<>(
						SELECT expires_at FROM operator_demo_group_claims WHERE id=$1)`, claim) != 0 {
					t.Fatal("own follow-up edit needed another proof or extended its deadline", restored, err)
				}
			})
		}
	}
}

func TestPostgresHumanGroupBusinessAccessClaimContinuationIsAtomicAndPersonal(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, scenario := range []string{"rollback", "other_actor"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				f := newGroupBusinessAccessFixture(t, platform)
				actor, _ := groupBusinessAccessOperator(t, f, 95)
				groupBusinessAccessSetOwnership(t, f, "unverified")
				claim := groupBusinessAccessClaim(t, f, actor, false)
				before := groupBusinessAccessSnapshot(t, f)
				if scenario == "rollback" {
					tx, err := f.f.db.Platform.Begin(f.f.ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback(f.f.ctx)
					_, err = SaveFeaturesTx(f.f.ctx, tx, actor, f.assistant.Catalog, f.scope.GroupID, before.Version,
						FeatureChanges{"blocked_words": {"enabled": json.RawMessage(`true`)}}, false)
					if err != nil {
						t.Fatal(err)
					}
					if err = tx.Rollback(f.f.ctx); err != nil {
						t.Fatal(err)
					}
					if groupBusinessAccessSnapshot(t, f) != before ||
						f.f.count(`SELECT count(*) FROM operator_demo_group_claims`) != 1 ||
						f.f.count(`SELECT count(*) FROM operator_demo_group_claims WHERE id=$1 AND state='active'`, claim) != 1 ||
						f.f.count(`SELECT count(*) FROM audit_events WHERE action='operator_demo_group_claim_config_continued'`) != 0 {
						t.Fatal("rolled-back configuration left a successor or changed its original proof")
					}
					return
				}
				other, _ := groupBusinessAccessOperator(t, f, 97)
				groupBusinessAccessClaim(t, f, other, false)
				saved, err := groupBusinessAccessSave(t, f, other, "fields", before.Version)
				if err != nil || saved.Version != before.Version+1 {
					t.Fatal(err)
				}
				requireGroupBusinessAccess(t, f, actor, HumanGroupAccess{OwnershipState: "unverified",
					AccessMode: "read_only", BlockedReason: "operator_demo_claim_required"})
				requireGroupBusinessAccess(t, f, other, HumanGroupAccess{OwnershipState: "unverified", AccessMode: "manage"})
				if f.f.count(`SELECT count(*) FROM operator_demo_group_claims WHERE actor_id=$1`, actor.ID) != 1 ||
					f.f.count(`SELECT count(*) FROM operator_demo_group_claims WHERE actor_id=$1`, other.ID) != 2 {
					t.Fatal("one actor's configuration carried another actor's proof")
				}
			})
		}
	}
}

func TestPostgresHumanGroupBusinessAccessDemoToggleAndCancel(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, state := range []string{"unverified", "paused"} {
			t.Run(platform+"/"+state, func(t *testing.T) {
				f := newGroupBusinessAccessFixture(t, platform)
				actor, ref := groupBusinessAccessOperator(t, f, 99)
				groupBusinessAccessSetOwnership(t, f, state)
				original := groupBusinessAccessClaim(t, f, actor, false)
				service := &GroupBindingService{DB: f.f.db}
				initial, err := service.OperatorDemoClaim(f.f.ctx, ref, f.scope.GroupID)
				if err != nil || initial.ID != original || initial.ExpiresAt == nil {
					t.Fatal(initial, err)
				}
				for _, enabled := range []bool{true, false} {
					before := groupBusinessAccessSnapshot(t, f)
					tx, err := f.f.db.Platform.Begin(f.f.ctx)
					if err != nil {
						t.Fatal(err)
					}
					raw, _ := json.Marshal(enabled)
					saved, err := SaveFeaturesTx(f.f.ctx, tx, actor, f.assistant.Catalog, f.scope.GroupID, before.Version,
						FeatureChanges{"blocked_words": {"enabled": raw}}, false)
					if err == nil {
						err = tx.Commit(f.f.ctx)
					} else {
						tx.Rollback(f.f.ctx)
					}
					current, claimErr := service.OperatorDemoClaim(f.f.ctx, ref, f.scope.GroupID)
					var config features.Config
					after := groupBusinessAccessSnapshot(t, f)
					if err != nil || saved.Version != before.Version+1 || claimErr != nil ||
						current.ID == original || current.State != "active" || current.ExpiresAt == nil ||
						!current.ExpiresAt.Equal(*initial.ExpiresAt) ||
						json.Unmarshal([]byte(after.Config), &config) != nil || config.FeatureOptions["blocked_words"].Enabled != enabled ||
						after.Management != before.Management || after.AI != before.AI {
						t.Fatal("on/off did not retain a bounded personal claim", enabled, saved, current, err, claimErr)
					}
				}
				_, err = service.CancelOperatorDemoClaim(f.f.ctx, ref, f.scope.GroupID, original, initial.Version)
				capabilityError(t, err, "operator_demo_claim_scope_changed")
				current, err := service.OperatorDemoClaim(f.f.ctx, ref, f.scope.GroupID)
				if err != nil {
					t.Fatal(err)
				}
				cancelled, err := service.CancelOperatorDemoClaim(f.f.ctx, ref, f.scope.GroupID, current.ID, current.Version)
				if err != nil || cancelled.State != "cancelled" ||
					f.f.count(`SELECT count(*) FROM operator_demo_group_claims WHERE actor_id=$1 AND state='active'`, actor.ID) != 0 {
					t.Fatal("cancel did not remove the current personal scope", cancelled, err)
				}
				requireGroupBusinessAccess(t, f, actor, HumanGroupAccess{OwnershipState: state,
					AccessMode: "read_only", BlockedReason: "operator_demo_claim_required"})
			})
		}
	}
}

func TestPostgresHumanGroupBusinessAccessClaimContinuationSerializesRevocation(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, cancel := range []bool{true, false} {
			name := "native_revocation"
			if cancel {
				name = "cancel_old_id"
			}
			t.Run(platform+"/"+name, func(t *testing.T) {
				f := newGroupBusinessAccessFixture(t, platform)
				actor, ref := groupBusinessAccessOperator(t, f, 103)
				groupBusinessAccessSetOwnership(t, f, "unverified")
				claim := groupBusinessAccessClaim(t, f, actor, false)
				session, err := f.assistant.CreateSession(f.f.ctx, ref, f.scope)
				if err != nil {
					t.Fatal(err)
				}
				plan := groupBusinessAccessActionPlan(t, f, ref, session)
				tx, err := f.f.db.Platform.Begin(f.f.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(f.f.ctx)
				_, err = SaveFeaturesTx(f.f.ctx, tx, actor, f.assistant.Catalog, f.scope.GroupID, 1,
					FeatureChanges{"blocked_words": {"enabled": json.RawMessage(`false`)}}, false)
				if err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				ctx, stop := context.WithTimeout(f.f.ctx, 5*time.Second)
				defer stop()
				go func() {
					if cancel {
						service := &GroupBindingService{DB: f.f.db}
						_, err := service.CancelOperatorDemoClaim(ctx, ref, f.scope.GroupID, claim, 1)
						done <- err
					} else {
						business := &assistantBusiness{service: f.assistant, plan: plan}
						done <- business.revokeDemoOwnership(ctx)
					}
				}()
				select {
				case err := <-done:
					t.Fatal("revocation bypassed the configuration lifecycle lock", err)
				case <-time.After(100 * time.Millisecond):
				}
				if err = tx.Commit(f.f.ctx); err != nil {
					t.Fatal(err)
				}
				err = <-done
				if cancel {
					capabilityError(t, err, "operator_demo_claim_scope_changed")
				} else if err != nil {
					t.Fatal(err)
				}
				active := f.f.count(`SELECT count(*) FROM operator_demo_group_claims
					WHERE actor_id=$1 AND state='active'`, actor.ID)
				if cancel && active != 1 || !cancel && active != 0 {
					t.Fatal("revocation missed the current successor or cancelled an unrelated claim", active)
				}
			})
		}
	}
}

func groupBusinessAccessActionPlan(t *testing.T, f *assistantBusinessFixture, ref SessionRef, session AssistantSession) AssistantActionPlan {
	t.Helper()
	fingerprint, err := f.assistant.authorizationHash(f.f.ctx, ref, session.Scope)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(assistantTarget{
		Actor: session.Actor.ID, Tenant: session.Actor.TenantID, Scope: session.Scope,
		Member:      AssistantMember{ID: "30001", Name: "synthetic owned member", Role: "member"},
		Fingerprint: fingerprint, ExpiresAt: time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := f.assistant.Vault.Seal("assistant:target", raw)
	clear(raw)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := f.assistant.proposeAction(f.f.ctx, ref, session, "mute_member",
		base64.RawURLEncoding.EncodeToString(sealed), "", 1)
	if err != nil || plan.Status != "pending_approval" || !plan.Production {
		t.Fatal("synthetic action was not eligible before ownership revocation", plan, err)
	}
	return plan
}

func TestPostgresHumanGroupBusinessAccessRevocationRejectsOldApprovals(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, source := range []string{"customer_missing", "customer_paused", "operator_claim"} {
			t.Run(platform+"/"+source, func(t *testing.T) {
				f := newGroupBusinessAccessFixture(t, platform)
				ref := f.ref
				claim := ""
				if source == "operator_claim" {
					actor, operatorRef := groupBusinessAccessOperator(t, f, 95)
					ref = operatorRef
					groupBusinessAccessSetOwnership(t, f, "unverified")
					claim = groupBusinessAccessClaim(t, f, actor, false)
				}
				session, err := f.assistant.CreateSession(f.f.ctx, ref, f.scope)
				if err != nil {
					t.Fatal(err)
				}
				result, err := f.assistant.propose(f.f.ctx, ref, session, assistantOutcomesConfigCall(t, f))
				config, ok := result.Data.(AssistantPlan)
				if err != nil || !ok || config.Status != "pending_approval" {
					t.Fatal("synthetic configuration was not pending before revocation", result, err)
				}
				action := groupBusinessAccessActionPlan(t, f, ref, session)
				before := groupBusinessAccessSnapshot(t, f)
				code := "group_ownership_proof_required"
				switch source {
				case "customer_missing":
					groupBusinessAccessSetOwnership(t, f, "unverified")
				case "customer_paused":
					groupBusinessAccessSetOwnership(t, f, "paused")
				case "operator_claim":
					f.f.exec(`UPDATE operator_demo_group_claims SET state='cancelled',version=version+1 WHERE id=$1`, claim)
					code = "operator_demo_claim_required"
				}
				_, err = f.assistant.Approve(f.f.ctx, ref, config.ID, config.Digest, config.Version)
				capabilityError(t, err, code)
				_, err = f.assistant.ApproveAction(f.f.ctx, ref, action.ID, action.Digest, action.Version, "synthetic-revoked-group-approval")
				capabilityError(t, err, code)
				if groupBusinessAccessSnapshot(t, f) != before ||
					f.f.count(`SELECT count(*) FROM assistant_plan_executions`) != 0 ||
					f.f.count(`SELECT count(*) FROM assistant_plans WHERE id=$1 AND status='pending_approval'
						AND version=$2 AND execution_id IS NULL`, config.ID, config.Version) != 1 ||
					f.f.count(`SELECT count(*) FROM assistant_action_plans WHERE id=$1 AND status='pending_approval'
						AND version=$2 AND operation_id IS NULL AND idempotency_key IS NULL`, action.ID, action.Version) != 1 ||
					f.f.count(`SELECT count(*) FROM audit_events WHERE action IN
						('assistant_plan_committed','assistant_action_approved')`) != 0 {
					t.Fatal("revoked ownership admitted an old confirmation")
				}
			})
		}
	}
}
