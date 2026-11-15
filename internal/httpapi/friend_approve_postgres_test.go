package httpapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/nativeauth"
)

func testFriendApprovalDurability(t *testing.T, service *control.NativeSettingsService,
	ref control.SessionRef, req control.SettingRequest, baseProof func() control.SettingEvidence) {
	t.Helper()
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := service.DB.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	var actor, group string
	if err := service.DB.Platform.QueryRow(ctx, `SELECT actor_id::text,group_id::text FROM test_windows WHERE id=$1`,
		req.TestWindowID).Scan(&actor, &group); err != nil {
		t.Fatal(err)
	}
	req.Change = nativeauth.SettingChange{Kind: "friend_approve", Group: req.Change.Group, Target: "8", Text: "1700000000123"}
	epochs, _ := json.Marshal(map[string]int64{req.Account: req.Epoch})
	newWindow := func() string {
		t.Helper()
		id := control.NewID()
		exec(`INSERT INTO test_windows(id,actor_id,group_id,account_ids,target_ids,actions,account_epochs,expires_at,budget,admission_mode,friend_application_id)
			VALUES($1,$2,$3,ARRAY[$4::uuid],ARRAY[$5],ARRAY['friend_approve'],$6,now()+interval '300 seconds',1,'native_debug',$7)`,
			id, actor, group, req.Account, req.Change.Target, epochs, req.Change.Text)
		return id
	}
	reset := func() {
		exec(`UPDATE native_setting_operations SET updated_at=now()-interval '6 seconds' WHERE account_id=$1`, req.Account)
		req.Key, req.TestWindowID = control.NewID(), newWindow()
	}
	reset()
	if _, err := service.TestScope(ctx, ref, req.Workspace, req.TestWindowID, req.Account, "friend_approve", req.Change.Target); err == nil {
		t.Fatal("UID-only scope admitted approval")
	}
	if _, err := service.TestScopeForApplication(ctx, ref, req.Workspace, req.TestWindowID, req.Account, "friend_approve", req.Change.Target, "1700000000124"); err == nil {
		t.Fatal("different application admitted")
	}
	if _, err := service.TestScopeForApplication(ctx, ref, req.Workspace, req.TestWindowID, req.Account, "friend_approve", req.Change.Target, req.Change.Text); err != nil {
		t.Fatal("exact application was rejected", err)
	}
	for _, query := range []string{
		`UPDATE test_windows SET budget=2 WHERE id=$1`,
		`UPDATE test_windows SET admission_mode='package' WHERE id=$1`,
		`UPDATE test_windows SET friend_application_id='' WHERE id=$1`,
		`UPDATE test_windows SET target_ids=ARRAY['8','9'] WHERE id=$1`,
	} {
		if _, err := service.DB.Platform.Exec(ctx, query, req.TestWindowID); err == nil {
			t.Fatal("database widened approval", query)
		}
	}
	submits := 0
	proof := func() control.SettingEvidence {
		p := baseProof()
		p.Role, p.Target, p.ObservedAt = "admin", req.Change.Target, time.Now()
		return p
	}
	port := func(work context.Context, fence control.SettingFence) error {
		return fence(work, proof(), func(context.Context) error { submits++; return nil })
	}
	noWindow := req
	noWindow.TestWindowID = ""
	if _, err := service.ExecuteSocial(ctx, ref, noWindow, port); err == nil {
		t.Fatal("ordinary route approved a friend")
	}
	req.Change.Text = "1700000000124"
	if _, err := service.ExecuteSocial(ctx, ref, req, port); err == nil || submits != 0 {
		t.Fatal("another application reached submission", err)
	}
	req.Change.Text = "1700000000123"
	for _, pair := range [][2]string{
		{`UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, `UPDATE accounts SET epoch=epoch-1 WHERE id=$1`},
		{`UPDATE account_receivers SET enabled=false WHERE account_id=$1`, `UPDATE account_receivers SET enabled=true,observed_at=now() WHERE account_id=$1`},
	} {
		exec(pair[0], req.Account)
		if _, err := service.ExecuteSocial(ctx, ref, req, port); err == nil || submits != 0 {
			t.Fatal("changed account admitted approval", err)
		}
		exec(pair[1], req.Account)
	}
	out, err := service.ExecuteSocial(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, req.TestWindowID)
		return port(work, fence)
	})
	if err != nil || out.Status != "failed" || submits != 0 {
		t.Fatal("cancelled approval passed final fence", out, err)
	}
	reset()
	out, err = service.ExecuteSocial(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		if err := port(work, fence); err != nil {
			return err
		}
		return &nativeauth.Error{Code: "native_transport_unknown", Submitted: true}
	})
	if err != nil || out.Status != "unknown" || submits != 1 {
		t.Fatal("unknown approval was not retained", out, err)
	}
	restarted := &control.NativeSettingsService{DB: service.DB, Vault: service.Vault}
	again, err := restarted.ExecuteSocial(ctx, ref, req, port)
	if err != nil || again.ID != out.ID || submits != 1 {
		t.Fatal("restarted service replayed approval", again, err)
	}
	exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, req.TestWindowID)
	observed, err := restarted.Reconcile(ctx, ref, req.Account, out.ID,
		func(context.Context, control.SettingRequest) (control.SettingEvidence, error) {
			p := proof()
			p.Matches = true
			return p, nil
		})
	if err != nil || observed.Code != "desired_state_observed" || submits != 1 {
		t.Fatal("unknown approval did not reconcile read-only", observed, err)
	}
	reset()
	out, err = service.ExecuteSocial(ctx, ref, req, port)
	if err != nil || out.Status != "succeeded" || submits != 2 {
		t.Fatal("exact approval failed", out, err)
	}
	req.Key = control.NewID()
	if _, err := service.ExecuteSocial(ctx, ref, req, port); err == nil || submits != 2 {
		t.Fatal("spent approval window reused", err)
	}
}
