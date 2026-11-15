package control

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wangshangliao/internal/assistant"
	"wangshangliao/internal/features"
)

type assistantBusinessFixture struct {
	f         *businessFixture
	assistant *AssistantService
	native    *NativeSettingsService
	ref       SessionRef
	scope     assistant.Scope
	nativeID  string
	session   string
}

func newAssistantBusinessFixture(t *testing.T, platform string) *assistantBusinessFixture {
	t.Helper()
	f := newBusinessFixture(t)
	ids := []string{}
	for _, d := range features.AssistantCapabilities(platform) {
		if d.Implemented {
			ids = append(ids, d.ID)
		}
	}
	f.exec(`UPDATE assistant_module_exposure SET capabilities=$2 WHERE platform=$1`, platform, ids)
	f.exec(`UPDATE plans SET modules=ARRAY['moderation','replies','cards','management_assistant'] WHERE id=$1`, f.plan)
	f.exec(`UPDATE assistant_settings SET enabled=true,tenant_access=true,approvals_enabled=true`)
	account := f.account(platform, 1)
	scope := f.group(platform, account, f.tenant, 1, false)
	f.exec(`UPDATE group_business_runtime SET enabled=true WHERE group_id=$1`, scope.Group)
	f.exec(`INSERT INTO business_capability_gates(platform,action,source,state,evidence_refs)
		VALUES($1,'mute_member','assistant','open',ARRAY['acceptance:SYNTHETIC-ASSISTANT'])`, platform)
	ref := SessionRef{hash: bytes.Repeat([]byte{19}, 32), platform: platform}
	f.exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at)
		VALUES($1,$2,$3,clock_timestamp()+interval '1 hour')`, ref.hash, bytes.Repeat([]byte{20}, 32), f.actor)
	session := NewID()
	f.exec(`INSERT INTO assistant_sessions(id,actor_id,tenant_id,domain,scope_kind,account_id,group_id)
		VALUES($1,$2,$3,$4,'group',$5,$6)`, session, f.actor, f.tenant, platform, account, scope.Group)
	out := &assistantBusinessFixture{
		f: f, assistant: &AssistantService{DB: f.db, Vault: f.service.Vault, Catalog: BusinessPermissionCatalog()},
		native: &NativeSettingsService{DB: f.db, Vault: f.service.Vault}, ref: ref,
		scope:    assistant.Scope{Platform: platform, Kind: "group", GroupID: scope.Group, AccountID: account},
		nativeID: scope.NativeGroup, session: session,
	}
	out.anchor()
	return out
}

func (f *assistantBusinessFixture) anchor() {
	f.f.t.Helper()
	challenge := NewID()
	f.f.exec(`INSERT INTO group_binding_challenges(id,tenant_id,applicant_id,platform,account_id,
		account_epoch,account_version,applicant_version,token_hash,private_token_hash,create_key,request_hash,state,
		group_id,proof_event_id,proof_sealed,proof_role)
		VALUES($1,$2,$3,$4,$5,1,1,1,$6,$9,$1::uuid::text,$6,'bound',$7,1,$8,'owner')`,
		challenge, f.f.tenant, f.f.actor, f.scope.Platform, f.scope.AccountID,
		bytes.Repeat([]byte{21}, 32), f.scope.GroupID, []byte("synthetic-proof"), bytes.Repeat([]byte{22}, 32))
	tx, err := f.f.db.Platform.Begin(f.f.ctx)
	if err != nil {
		f.f.t.Fatal(err)
	}
	defer tx.Rollback(f.f.ctx)
	event := int64(1)
	bindings := GroupBindingService{DB: f.f.db, Vault: f.f.service.Vault}
	err = bindings.saveOwnership(f.f.ctx, tx,
		bindingRecord{ID: challenge, Group: f.scope.GroupID, Tenant: f.f.tenant, Platform: f.scope.Platform, ProofEvent: &event},
		bindingProof{Event: event, Owner: "native-owner", Sender: "native-owner", SenderRole: "owner", RobotRole: "admin"})
	if err != nil {
		f.f.t.Fatal(err)
	}
	if err = tx.Commit(f.f.ctx); err != nil {
		f.f.t.Fatal(err)
	}
}

func (f *assistantBusinessFixture) request() SettingRequest {
	f.f.t.Helper()
	// Advance synthetic prior writes past the account throttle without sleeping.
	f.f.exec(`UPDATE native_setting_operations SET updated_at=clock_timestamp()-interval '10 seconds'
		WHERE account_id=$1`, f.scope.AccountID)
	p := AssistantActionPlan{
		ID: NewID(), SessionID: f.session, Scope: f.scope, Action: "mute_member",
		Digest: strings.Repeat("a", 64), Status: "approved", Minutes: 1, ExecutionMode: "legacy",
		Target: &AssistantMember{ID: "30001", Role: "member"},
	}
	p.PolicyVersion = 1
	if err := f.f.db.Platform.QueryRow(f.f.ctx, `SELECT b.version,b.epoch,a.epoch
		FROM group_bindings b JOIN accounts a ON a.id=b.primary_account_id WHERE b.group_id=$1`,
		f.scope.GroupID).Scan(&p.BaseVersion, &p.BindingEpoch, &p.AccountEpoch); err != nil {
		f.f.t.Fatal(err)
	}
	payload, _ := json.Marshal(assistantActionPayload{Plan: p, AccountEpoch: p.AccountEpoch, BindingEpoch: p.BindingEpoch})
	sealed, err := f.f.service.Vault.Seal("assistant:action:"+p.ID, payload)
	clear(payload)
	if err != nil {
		f.f.t.Fatal(err)
	}
	f.f.exec(`INSERT INTO assistant_action_plans(id,session_id,actor_id,tenant_id,domain,account_id,group_id,
		action,digest,authorization_hash,payload_sealed,account_epoch,binding_epoch,base_version,status)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'synthetic',$10,$11,$12,$13,'approved')`,
		p.ID, p.SessionID, f.f.actor, f.f.tenant, f.scope.Platform, f.scope.AccountID, f.scope.GroupID,
		p.Action, p.Digest, sealed, p.AccountEpoch, p.BindingEpoch, p.BaseVersion)
	return SettingRequest{Account: f.scope.AccountID, Workspace: f.scope.Platform, Key: p.ID,
		Epoch: p.AccountEpoch, Version: 1, Digest: strings.Repeat("b", 64),
		Change: p.settingChange(f.nativeID), business: &assistantBusiness{service: f.assistant, plan: p}}
}

func (f *assistantBusinessFixture) proof(req SettingRequest) SettingEvidence {
	f.f.t.Helper()
	deployment := int64(0)
	if f.scope.Platform == "wsl" {
		deployment = 1
	}
	source, err := f.f.proof(f.f.ctx, BusinessScope{Platform: f.scope.Platform, Account: req.Account,
		NativeGroup: req.Change.Group, NativeSelf: "10001", AccountEpoch: req.Epoch,
		AccountVersion: req.Version, DeploymentVersion: deployment}, req.Change.Target)
	if err != nil {
		f.f.t.Fatal(err)
	}
	return SettingEvidence{
		UID: source.SelfID, Role: source.SelfRole, Target: source.Target, TargetRole: source.TargetRole,
		OwnerID: source.OwnerID, ReceiverOwner: "synthetic-native-owner", ReceiverRevision: 1,
		Digest: req.Digest, Epoch: req.Epoch, Version: req.Version, DeploymentVersion: deployment,
		CredentialDigest: source.CredentialDigest, ObservedAt: time.Now(), ModerationAllowed: true,
	}
}

func TestPostgresAssistantBusinessUsesAdmissionWithoutTestConnectionFlags(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newAssistantBusinessFixture(t, platform)
			var writes atomic.Int32
			req := f.request()
			execute := func(req SettingRequest, before func()) (SettingOperation, error) {
				return f.native.execute(f.f.ctx, f.ref, req, func(work context.Context, fence SettingFence) error {
					if before != nil {
						before()
					}
					return fence(work, f.proof(req), func(context.Context) error { writes.Add(1); return nil })
				}, nil)
			}
			op, err := execute(req, nil)
			if err != nil || op.Status != "succeeded" || writes.Load() != 1 {
				t.Fatal("formal admission depended on legacy test flag", op, err)
			}
			if f.f.count(`SELECT count(*) FROM accounts WHERE id=$1 AND NOT connection_enabled AND NOT testing_enabled`, req.Account) != 1 ||
				f.f.count(`SELECT count(*) FROM test_windows`) != 0 ||
				f.f.count(`SELECT count(*) FROM native_setting_operations WHERE id=$1 AND test_window_id IS NULL`, op.ID) != 1 {
				t.Fatal("formal action borrowed or changed test admission")
			}
			again, err := execute(req, nil)
			if err != nil || again.ID != op.ID || writes.Load() != 1 {
				t.Fatal("durable duplicate was replayed", again, err)
			}
			for _, row := range []struct {
				name, revoke, restore, code string
			}{
				{"gate", `UPDATE business_capability_gates SET state='closed' WHERE source='assistant'`,
					`UPDATE business_capability_gates SET state='open' WHERE source='assistant'`, "business_admission_closed"},
				{"runtime", `UPDATE group_business_runtime SET enabled=false`,
					`UPDATE group_business_runtime SET enabled=true`, "group_runtime_disabled"},
				{"ceiling", `UPDATE tenant_capability_ceilings SET permissions=array_remove(permissions,'group_action:mute_member')`,
					`UPDATE tenant_capability_ceilings SET permissions=array_append(permissions,'group_action:mute_member')`, "permission_denied"},
				{"receiver", `UPDATE account_receivers SET enabled=false`,
					`UPDATE account_receivers SET enabled=true`, "native_text_connection_changed"},
			} {
				t.Run(row.name, func(t *testing.T) {
					t.Cleanup(func() {
						if _, err := f.f.db.Platform.Exec(f.f.ctx, row.restore); err != nil {
							t.Errorf("restore %s fixture: %v", row.name, err)
						}
					})
					operation, err := execute(f.request(), func() { f.f.exec(row.revoke) })
					if err != nil || operation.Status != "failed" || operation.Code != row.code || writes.Load() != 1 {
						t.Fatal("revoked final scope reached synthetic native port", operation, err)
					}
				})
			}
			req = f.request()
			req.TestWindowID = NewID()
			if _, err = execute(req, nil); err == nil || writes.Load() != 1 {
				t.Fatal("formal admission accepted a test grant")
			}
			req = f.request()
			req.business = nil
			if _, err = execute(req, nil); err == nil || writes.Load() != 1 {
				t.Fatal("non-business request bypassed the test boundary")
			}
			for i, state := range []string{"executing", "submitting", "unknown"} {
				id := NewID()
				f.f.exec(`INSERT INTO business_rule_jobs(id,platform,tenant_id,actor_id,account_id,group_id,
					event_id,rule,action,priority,scope_snapshot,status,expires_at)
					VALUES($1,$2,$3,$4,$5,$6,$9,'keyword_replies','send_text',1,$7,$8,clock_timestamp()+interval '60 seconds')`,
					id, platform, f.f.tenant, f.f.actor, f.scope.AccountID, f.scope.GroupID, []byte{1}, state, i+1)
				_, err = execute(f.request(), nil)
				capabilityError(t, err, "business_account_busy")
				f.f.exec(`UPDATE business_rule_jobs SET status='failed' WHERE id=$1`, id)
			}
			f.f.exec(`INSERT INTO business_rule_account_cooldowns(account_id,next_write_at)
				VALUES($1,clock_timestamp()+interval '1 minute')`, req.Account)
			_, err = execute(f.request(), nil)
			capabilityError(t, err, "business_account_busy")
			if writes.Load() != 1 {
				t.Fatal("automatic work or cooldown overlapped assistant submission")
			}
		})
	}
}

func TestPostgresAssistantOwnershipChangePausesOutsideRejectedFence(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newAssistantBusinessFixture(t, platform)
			req := f.request()
			var writes atomic.Int32
			op, err := f.native.execute(f.f.ctx, f.ref, req, func(ctx context.Context, fence SettingFence) error {
				proof := f.proof(req)
				proof.OwnerID = "new-native-owner"
				return fence(ctx, proof, func(context.Context) error { writes.Add(1); return nil })
			}, nil)
			if err != nil || op.Status != "failed" || op.Code != "group_ownership_changed" || writes.Load() != 0 {
				t.Fatal("owner transfer crossed final fence", op, err)
			}
			if f.f.count(`SELECT count(*) FROM group_binding_ownership WHERE group_id=$1 AND state='paused'`, f.scope.GroupID) != 1 ||
				f.f.count(`SELECT count(*) FROM group_bindings WHERE group_id=$1 AND NOT management_enabled AND NOT ai_enabled`, f.scope.GroupID) != 1 ||
				f.f.count(`SELECT count(*) FROM group_business_runtime WHERE group_id=$1 AND NOT enabled`, f.scope.GroupID) != 1 {
				t.Fatal("rejected submission rolled back owner-transfer pause")
			}
		})
	}
}
