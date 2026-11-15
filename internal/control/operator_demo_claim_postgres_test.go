package control

import (
	"testing"
)

func (o *operatorEntitlementFixture) demoClaim() string {
	f, scope := o.base.f, o.base.scope
	id := NewID()
	f.exec(`INSERT INTO operator_demo_group_claims(id,actor_id,actor_version,tenant_id,group_id,platform,
		account_id,account_epoch,account_version,binding_epoch,binding_version,create_key,state,robot_role,owner_identity_hash)
		SELECT $1,u.id,u.access_version,b.tenant_id,b.group_id,a.platform,a.id,a.epoch,a.version,b.epoch,b.version,
		$1::uuid::text,'active','admin',$4 FROM group_bindings b JOIN accounts a ON a.id=b.primary_account_id
		JOIN users u ON u.id=$2 WHERE b.group_id=$3`, id, o.admin.ID, scope.GroupID,
		bindingIdentityDigest(scope.Platform, scope.GroupID, "native-owner"))
	return id
}

func TestPostgresOperatorDemoClaimFencesNativeWritesWithoutCustomerProof(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, scenario := range []string{"success", "cancel", "gate", "account_epoch", "actor_version", "owner", "role"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				o := newOperatorEntitlementFixture(t, platform)
				b, f := o.base, o.base.f
				f.exec(`DELETE FROM group_binding_ownership WHERE group_id=$1`, b.scope.GroupID)
				f.exec(`DELETE FROM subscriptions WHERE tenant_id=$1`, f.tenant)
				f.exec(`UPDATE group_business_runtime SET enabled=false WHERE group_id=$1`, b.scope.GroupID)
				f.exec(`UPDATE group_bindings SET management_enabled=false WHERE group_id=$1`, b.scope.GroupID)
				o.gate("manual", "open")
				blocked := o.mutePlan()
				if blocked.Production || blocked.ResultCode != "operator_demo_claim_required" {
					t.Fatal("unverified legacy binding was admitted", blocked)
				}
				claim := o.demoClaim()
				p := o.mutePlan()
				if !p.Production {
					t.Fatal("demo claim still required a customer proof or package", p)
				}
				o.beforeWrite = func() {
					switch scenario {
					case "cancel":
						f.exec(`UPDATE operator_demo_group_claims SET state='cancelled',version=version+1 WHERE id=$1`, claim)
					case "gate":
						o.gate("manual", "closed")
					case "account_epoch":
						f.exec(`UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, b.scope.AccountID)
					case "actor_version":
						f.exec(`UPDATE users SET access_version=access_version+1 WHERE id=$1`, o.admin.ID)
					}
				}
				o.proofChange = func(proof *SettingEvidence) {
					if scenario == "owner" {
						proof.OwnerID = "synthetic-new-owner"
					}
					if scenario == "role" {
						proof.Role = "member"
					}
				}
				result, _ := o.manual.Approve(f.ctx, o.ref, b.scope.GroupID, p.ID, p.Digest, p.Version, "synthetic-demo-final")
				if scenario == "success" {
					if result.Status != "succeeded" || o.writes.Load() != 1 {
						t.Fatal("verified operator could not execute once", result)
					}
					again, err := o.manual.Approve(f.ctx, o.ref, b.scope.GroupID, p.ID, p.Digest, p.Version, "synthetic-demo-final")
					if err != nil || again.OperationID != result.OperationID || o.writes.Load() != 1 {
						t.Fatal("duplicate approval repeated a native write", err)
					}
				} else if o.writes.Load() != 0 || f.count(`SELECT count(*) FROM native_setting_operations WHERE status='failed'`) != 1 {
					t.Fatal("changed scope crossed the final fence", scenario, result)
				}
				if (scenario == "owner" || scenario == "role") &&
					f.count(`SELECT count(*) FROM operator_demo_group_claims WHERE id=$1 AND state='cancelled'`, claim) != 1 {
					t.Fatal("changed native authority retained a demo claim")
				}
				if f.count(`SELECT count(*) FROM group_binding_ownership`) != 0 ||
					f.count(`SELECT count(*) FROM subscriptions WHERE tenant_id=$1`, f.tenant) != 0 ||
					f.count(`SELECT count(*) FROM group_business_runtime WHERE group_id=$1 AND enabled`, b.scope.GroupID) != 0 ||
					f.count(`SELECT count(*) FROM test_windows`) != 0 {
					t.Fatal("demo manufactured customer ownership, package, runtime or debug grant")
				}
			})
		}
	}
}
