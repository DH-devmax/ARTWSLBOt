package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"wangshangliao/internal/security"
)

func configRoleFixtureProof(t *testing.T, f *assistantBusinessFixture, scope GroupBindingNativeScope) GroupBindingRoleEvidence {
	t.Helper()
	out := GroupBindingRoleEvidence{
		GroupBindingConnectionEvidence: GroupBindingConnectionEvidence{
			Platform: scope.Platform, AccountID: scope.AccountID, AccountEpoch: scope.AccountEpoch,
			AccountVersion: scope.AccountVersion, ObservedAt: time.Now(),
		},
		NativeGroupID: scope.NativeGroupID, NativeSenderID: scope.NativeSenderID,
		NativeOwnerID: "native-owner", SenderRole: "admin", RobotRole: "admin", Present: true,
	}
	var credential []byte
	if err := f.f.db.Platform.QueryRow(f.f.ctx, `SELECT credential_sealed FROM accounts WHERE id=$1`,
		scope.AccountID).Scan(&credential); err != nil {
		t.Fatal(err)
	}
	copy(out.CredentialDigest[:], security.Digest(string(credential)))
	if err := f.f.db.Platform.QueryRow(f.f.ctx, `SELECT owner,revision FROM account_receivers WHERE account_id=$1`,
		scope.AccountID).Scan(&out.ReceiverOwner, &out.ReceiverRevision); err != nil {
		t.Fatal(err)
	}
	if scope.Platform == "wsl" {
		if err := f.f.db.Platform.QueryRow(f.f.ctx, `SELECT version FROM native_deployment WHERE singleton`).
			Scan(&out.DeploymentVersion); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func configRoleFixturePlan(t *testing.T, f *assistantBusinessFixture, ref SessionRef) AssistantPlan {
	t.Helper()
	session, err := f.assistant.CreateSession(f.f.ctx, ref, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.assistant.propose(f.f.ctx, ref, session, assistantOutcomesConfigCall(t, f))
	plan, ok := result.Data.(AssistantPlan)
	if err != nil || !ok {
		t.Fatal("synthetic configuration proposal", err)
	}
	f.f.exec(`UPDATE account_groups SET observed_at=clock_timestamp()-interval '61 seconds'
		WHERE account_id=$1 AND group_id=$2`, f.scope.AccountID, f.scope.GroupID)
	return plan
}

func TestPostgresAssistantConfigRoleRefresh(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, mode := range []string{"tenant", "operator_demo"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				f := newGroupBusinessAccessFixture(t, platform)
				ref := f.ref
				claim := ""
				if mode == "operator_demo" {
					actor, demo := groupBusinessAccessOperator(t, f, 95)
					ref = demo
					groupBusinessAccessSetOwnership(t, f, "unverified")
					claim = groupBusinessAccessClaim(t, f, actor, false)
				}
				p := configRoleFixturePlan(t, f, ref)
				reads := 0
				f.assistant.ConfigRoles = func(ctx context.Context, scope GroupBindingNativeScope) (GroupBindingRoleEvidence, error) {
					reads++
					if scope.Platform != platform || scope.AccountID != f.scope.AccountID || scope.NativeGroupID != f.nativeID ||
						scope.AccountEpoch != p.AccountEpoch || scope.NativeSenderID == "" {
						t.Fatal("configuration refresh escaped server-resolved scope")
					}
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > 8*time.Second {
						t.Fatal("configuration refresh is not bounded")
					}
					return configRoleFixtureProof(t, f, scope), nil
				}
				committed, err := f.assistant.Approve(f.f.ctx, ref, p.ID, p.Digest, p.Version)
				if err != nil || committed.Status != "committed" || committed.Applied ||
					committed.CommittedVersion != p.BaseVersion+1 || reads != 1 {
					t.Fatal("fresh unchanged authority did not save exactly once", committed, reads, err)
				}
				f.assistant.ConfigRoles = func(context.Context, GroupBindingNativeScope) (GroupBindingRoleEvidence, error) {
					t.Fatal("committed reconciliation repeated a native read")
					return GroupBindingRoleEvidence{}, errors.New("synthetic-unreachable")
				}
				replayed, err := f.assistant.Approve(f.f.ctx, ref, p.ID, p.Digest, p.Version)
				if err != nil || replayed.ExecutionID != committed.ExecutionID ||
					f.f.count(`SELECT count(*) FROM assistant_plan_executions WHERE plan_id=$1`, p.ID) != 1 {
					t.Fatal("configuration reconciliation changed its execution", err)
				}
				if claim != "" && f.f.count(`SELECT count(*) FROM operator_demo_group_claims old
					JOIN operator_demo_group_claims current ON current.actor_id=old.actor_id AND current.group_id=old.group_id
					WHERE old.id=$1 AND old.state='cancelled' AND current.state='active'
					AND current.expires_at=old.expires_at AND current.binding_version=old.binding_version+1`, claim) != 1 {
					t.Fatal("role refresh extended the demo deadline or lost its successor")
				}
			})
		}
	}
}

func TestPostgresAssistantConfigRoleRefreshRejectsChangedScope(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, scenario := range []string{"unavailable", "cancelled", "demoted", "wrong_group", "wrong_platform",
			"old_proof", "superseded", "owner_changed", "receiver_changed", "account_version", "binding_epoch",
			"policy_closed", "session_revoked", "no_approval", "digest"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				f := newGroupBusinessAccessFixture(t, platform)
				p := configRoleFixturePlan(t, f, f.ref)
				before := groupBusinessAccessSnapshot(t, f)
				reads := 0
				f.assistant.ConfigRoles = func(ctx context.Context, scope GroupBindingNativeScope) (GroupBindingRoleEvidence, error) {
					reads++
					proof := configRoleFixtureProof(t, f, scope)
					switch scenario {
					case "unavailable":
						return proof, Denied(503, "synthetic_role_unavailable")
					case "cancelled":
						return proof, context.Canceled
					case "demoted":
						proof.RobotRole, proof.SenderRole = "member", "member"
					case "wrong_group":
						proof.NativeGroupID = "another-native-group"
					case "wrong_platform":
						proof.Platform = "other"
					case "old_proof":
						proof.ObservedAt = time.Now().Add(-6 * time.Second)
					case "superseded":
						f.f.exec(`UPDATE account_groups SET role='member',observed_at=clock_timestamp()
							WHERE account_id=$1 AND group_id=$2`, f.scope.AccountID, f.scope.GroupID)
					case "owner_changed":
						proof.NativeOwnerID = "another-owner"
					case "receiver_changed":
						f.f.exec(`UPDATE account_receivers SET owner='another-receiver' WHERE account_id=$1`, f.scope.AccountID)
					case "account_version":
						f.f.exec(`UPDATE accounts SET version=version+1 WHERE id=$1`, f.scope.AccountID)
					case "binding_epoch":
						f.f.exec(`UPDATE group_bindings SET epoch=epoch+1 WHERE group_id=$1`, f.scope.GroupID)
					case "policy_closed":
						f.f.exec(`UPDATE assistant_module_exposure SET capabilities='{}',version=version+1 WHERE platform=$1`, platform)
					case "session_revoked":
						f.f.exec(`UPDATE users SET access_version=access_version+1 WHERE id=$1`, f.f.actor)
					}
					return proof, nil
				}
				summary := p.Digest
				if scenario == "no_approval" {
					f.f.exec(`UPDATE user_access SET permissions=array_remove(permissions,'assistant:approve') WHERE user_id=$1`, f.f.actor)
				}
				if scenario == "digest" {
					summary = "wrong-digest"
				}
				_, err := f.assistant.Approve(f.f.ctx, f.ref, p.ID, summary, p.Version)
				wantReads := 1
				if scenario == "no_approval" || scenario == "digest" {
					wantReads = 0
				}
				after := groupBusinessAccessSnapshot(t, f)
				if err == nil || reads != wantReads || after.Version != before.Version || after.Config != before.Config ||
					f.f.count(`SELECT count(*) FROM assistant_plan_executions`) != 0 ||
					f.f.count(`SELECT count(*) FROM audit_events WHERE action='assistant_plan_committed'`) != 0 {
					t.Fatal("invalid role refresh saved a configuration or repeated native work", scenario, reads, err)
				}
			})
		}
	}
}
