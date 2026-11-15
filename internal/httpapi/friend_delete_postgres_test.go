package httpapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/nativeauth"
)

// Reuse the dual-platform invitation fixture's authenticated identity and proof.
func testFriendDeleteDurability(t *testing.T, service *control.NativeSettingsService,
	ref control.SessionRef, req control.SettingRequest, baseProof func() control.SettingEvidence) {
	t.Helper()
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := service.DB.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	target := "8"
	if req.Workspace == "lajiaoliao" {
		target = "friend-string"
	}
	req.Change = nativeauth.SettingChange{Kind: "friend_delete", Group: req.Change.Group, Target: target}
	var actor, group string
	if err := service.DB.Platform.QueryRow(ctx, `SELECT actor_id::text,group_id::text
		FROM test_windows WHERE id=$1`, req.TestWindowID).Scan(&actor, &group); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO account_receivers(account_id,enabled,owner,lease_until,snapshot,observed_at)
		VALUES($1,true,'synthetic-receiver',now()+interval '1 hour','{"state":"connected"}',now())`, req.Account)
	exec(`UPDATE native_setting_operations SET updated_at=now()-interval '6 seconds' WHERE account_id=$1`, req.Account)
	epochs, _ := json.Marshal(map[string]int64{req.Account: req.Epoch})
	newWindow := func() string {
		t.Helper()
		id := control.NewID()
		exec(`INSERT INTO test_windows(id,actor_id,group_id,account_ids,target_ids,actions,account_epochs,expires_at,budget,admission_mode)
			VALUES($1,$2,$3,ARRAY[$4::uuid],ARRAY[$5],ARRAY['friend_delete'],$6,now()+interval '300 seconds',1,'native_debug')`,
			id, actor, group, req.Account, target, epochs)
		return id
	}
	req.Key, req.TestWindowID = control.NewID(), newWindow()
	submissions := 0
	proof := func() control.SettingEvidence {
		p := baseProof()
		p.Role, p.Target, p.ObservedAt = "admin", req.Change.Target, time.Now()
		return p
	}
	port := func(work context.Context, fence control.SettingFence) error {
		return fence(work, proof(), func(context.Context) error { submissions++; return nil })
	}
	if _, err := service.TestScope(ctx, ref, req.Workspace, req.TestWindowID, req.Account, "read_membership", ""); err != nil {
		t.Fatal("delete-only grant could not read current friendship", err)
	}
	noWindow := req
	noWindow.TestWindowID = ""
	if _, err := service.ExecuteSocial(ctx, ref, noWindow, port); err == nil {
		t.Fatal("friend deletion bypassed a persisted isolated grant")
	}
	if _, err := service.Execute(ctx, ref, req, port); err == nil {
		t.Fatal("profile executor admitted friend deletion")
	}
	for _, update := range []string{
		`UPDATE test_windows SET budget=2 WHERE id=$1`,
		`UPDATE test_windows SET admission_mode='package' WHERE id=$1`,
		`UPDATE test_windows SET actions=ARRAY['friend_delete','group_invite'] WHERE id=$1`,
		`UPDATE test_windows SET target_ids=ARRAY['8','9'] WHERE id=$1`,
	} {
		if _, err := service.DB.Platform.Exec(ctx, update, req.TestWindowID); err == nil {
			t.Fatal("database admitted a mixed or reusable deletion grant", update)
		}
	}
	for _, pair := range [][2]string{
		{`UPDATE accounts SET testing_enabled=false WHERE id=$1`, `UPDATE accounts SET testing_enabled=true WHERE id=$1`},
		{`UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, `UPDATE accounts SET epoch=epoch-1 WHERE id=$1`},
		{`UPDATE account_receivers SET enabled=false WHERE account_id=$1`, `UPDATE account_receivers SET enabled=true WHERE account_id=$1`},
		{`UPDATE account_receivers SET owner='' WHERE account_id=$1`, `UPDATE account_receivers SET owner='synthetic-receiver' WHERE account_id=$1`},
		{`UPDATE account_receivers SET lease_until=now()-interval '1 second' WHERE account_id=$1`, `UPDATE account_receivers SET lease_until=now()+interval '1 hour' WHERE account_id=$1`},
		{`UPDATE account_receivers SET snapshot='{"state":"blocked"}' WHERE account_id=$1`, `UPDATE account_receivers SET snapshot='{"state":"connected"}' WHERE account_id=$1`},
		{`UPDATE account_receivers SET observed_at=now()-interval '16 seconds' WHERE account_id=$1`, `UPDATE account_receivers SET observed_at=now() WHERE account_id=$1`},
	} {
		exec(pair[0], req.Account)
		if _, err := service.ExecuteSocial(ctx, ref, req, port); err == nil || submissions != 0 {
			t.Fatal("revoked or disconnected account deleted a friend", pair[0], err)
		}
		exec(pair[1], req.Account)
	}
	oldTarget := req.Change.Target
	req.Change.Target = proof().UID
	if _, err := service.ExecuteSocial(ctx, ref, req, port); err == nil || submissions != 0 {
		t.Fatal("deletion targeted the robot itself", err)
	}
	req.Change.Target = oldTarget
	out, err := service.ExecuteSocial(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, req.TestWindowID)
		return port(work, fence)
	})
	if err != nil || out.Status != "failed" || submissions != 0 {
		t.Fatal("cancelled grant passed the final native fence", out, err)
	}
	exec(`UPDATE native_setting_operations SET updated_at=now()-interval '6 seconds' WHERE account_id=$1`, req.Account)
	req.Key, req.TestWindowID = control.NewID(), newWindow()
	out, err = service.ExecuteSocial(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		p := proof()
		p.Role = "member"
		return fence(work, p, func(context.Context) error { submissions++; return nil })
	})
	if err != nil || out.Status != "failed" || submissions != 0 {
		t.Fatal("role loss passed final deletion fence", out, err)
	}
	exec(`UPDATE native_setting_operations SET updated_at=now()-interval '6 seconds' WHERE account_id=$1`, req.Account)
	req.Key, req.TestWindowID = control.NewID(), newWindow()
	out, err = service.ExecuteSocial(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		if err := port(work, fence); err != nil {
			return err
		}
		return &nativeauth.Error{Code: "native_transport_unknown", Submitted: true}
	})
	if err != nil || out.Status != "unknown" || submissions != 1 {
		t.Fatal("ambiguous deletion lost its durable result", out, err)
	}
	again, err := service.ExecuteSocial(ctx, ref, req, port)
	if err != nil || again.ID != out.ID || submissions != 1 {
		t.Fatal("unknown deletion was resubmitted", again, err)
	}
	exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, req.TestWindowID)
	observed, err := service.Reconcile(ctx, ref, req.Account, out.ID,
		func(context.Context, control.SettingRequest) (control.SettingEvidence, error) {
			p := proof()
			p.Matches = false
			return p, nil
		})
	if err != nil || observed.Status != "unknown" || submissions != 1 {
		t.Fatal("incomplete friend list proved deletion", observed, err)
	}
	observed, err = service.Reconcile(ctx, ref, req.Account, out.ID,
		func(context.Context, control.SettingRequest) (control.SettingEvidence, error) {
			p := proof()
			p.Matches = true
			return p, nil
		})
	if err != nil || observed.Code != "desired_state_observed" || submissions != 1 {
		t.Fatal("closed grant could not reconcile the original deletion", observed, err)
	}
	var used int
	if err := service.DB.Platform.QueryRow(ctx, `SELECT used FROM test_windows WHERE id=$1`, req.TestWindowID).Scan(&used); err != nil || used != 1 {
		t.Fatal("unknown deletion refunded its attempt or charged a duplicate", used, err)
	}
	exec(`UPDATE test_windows SET cancelled=false WHERE id=$1`, req.TestWindowID)
	req.Key = control.NewID()
	if _, err := service.ExecuteSocial(ctx, ref, req, port); err == nil || submissions != 1 {
		t.Fatal("spent one-attempt window allowed another deletion", err)
	}
	exec(`UPDATE native_setting_operations SET updated_at=now()-interval '6 seconds' WHERE account_id=$1`, req.Account)
	req.Key, req.TestWindowID = control.NewID(), newWindow()
	out, err = service.ExecuteSocial(ctx, ref, req, port)
	if err != nil || out.Status != "succeeded" || out.Code != "native_friend_delete_acknowledged" || submissions != 2 {
		t.Fatal("known deletion acknowledgement was not preserved", out, err)
	}
	again, err = service.ExecuteSocial(ctx, ref, req, port)
	if err != nil || again.ID != out.ID || submissions != 2 {
		t.Fatal("successful deletion was repeated", again, err)
	}
}
