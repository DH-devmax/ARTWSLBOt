package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
)

func TestPostgresIndependentContactGrantAfterClaimClosure(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, action := range []string{"friend_delete", "friend_approve"} {
			for _, when := range []string{"before preparation", "before submission"} {
				t.Run(platform+"/"+action+"/"+when, func(t *testing.T) {
					f := newControlledExecutionFixture(t, platform, true, time.Hour)
					f.f.exec(`UPDATE accounts SET connection_enabled=true WHERE id=$1`, f.scope.AccountID)
					req := SettingRequest{
						Account: f.scope.AccountID, Workspace: platform, Key: NewID(),
						Epoch: 1, Version: 1, Digest: strings.Repeat("b", 64),
						Change: nativeauth.SettingChange{Kind: action, Group: f.nativeID, Target: "30001"},
					}
					if action == "friend_approve" {
						req.Change.Text = "1700000000123"
					}
					epochs, _ := json.Marshal(map[string]int64{req.Account: req.Epoch})
					window := func(kind, application string) string {
						id := NewID()
						f.f.exec(`INSERT INTO test_windows(id,actor_id,group_id,account_ids,target_ids,actions,
							account_epochs,expires_at,budget,admission_mode,friend_application_id)
							VALUES($1,$2,$3,ARRAY[$4::uuid],ARRAY[$5],ARRAY[$6],$7,
							now()+interval '300 seconds',1,'native_debug',$8)`,
							id, f.actor.ID, f.scope.GroupID, req.Account, req.Change.Target, kind, epochs, application)
						return id
					}
					req.TestWindowID = window(action, req.Change.Text)
					closeScope := func() {
						t.Helper()
						bindings := GroupBindingService{DB: f.f.db, Vault: f.f.service.Vault}
						if _, err := bindings.CloseControlledScope(f.f.ctx, f.ref, f.scopeID, 1); err != nil {
							t.Fatal(err)
						}
					}
					if when == "before preparation" {
						closeScope()
					}
					writes := 0
					port := func(work context.Context, fence SettingFence) error {
						if when == "before submission" {
							closeScope()
						}
						proof := f.proof(req)
						if err := f.f.db.Platform.QueryRow(work, `SELECT native_uid FROM accounts WHERE id=$1`,
							req.Account).Scan(&proof.UID); err != nil {
							t.Fatal(err)
						}
						proof.SocialAllowed = true
						return fence(work, proof, func(context.Context) error { writes++; return nil })
					}
					out, err := f.native.ExecuteSocial(f.f.ctx, f.ref, req, port)
					wantWrites, wantStatus := 1, "succeeded"
					if when == "before submission" {
						wantWrites, wantStatus = 0, "failed"
						if out.Code != "native_setting_group_binding_required" {
							t.Fatal("binding epoch change did not fence prepared contact work", out, err)
						}
					}
					if err != nil || out.Status != wantStatus || writes != wantWrites {
						t.Fatal("independent contact grant inherited closed claim proof", out, writes, err)
					}
					restarted := NativeSettingsService{DB: f.f.db, Vault: f.f.service.Vault}
					again, err := restarted.ExecuteSocial(f.f.ctx, f.ref, req, port)
					if err != nil || again.ID != out.ID || writes != wantWrites {
						t.Fatal("contact operation repeated after restart", again, writes, err)
					}
					if used := f.f.count(`SELECT used FROM test_windows WHERE id=$1`, req.TestWindowID); used != 1 {
						t.Fatal("independent attempt budget was not consumed exactly once", used)
					}
					assertControlledUsage(t, f.f, f.scopeID, 0, 0, 0)
					capabilityError(t, RequireControlledBinding(f.f.ctx, f.f.db.Platform, f.actor.ID,
						platform, f.scope.GroupID), "controlled_onboarding_scope_inactive")
					req.Key, req.TestWindowID = NewID(), window("mute_member", "")
					req.Change.Kind, req.Change.Text, req.Change.Minutes = "mute_member", "", 1
					_, err = f.native.ExecuteModeration(f.f.ctx, f.ref, req, port)
					capabilityError(t, err, "controlled_onboarding_scope_inactive")
					if writes != wantWrites {
						t.Fatal("contact exception enabled unrelated group action", writes)
					}
				})
			}
		}
	}
}
