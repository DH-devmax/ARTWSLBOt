package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

type syntheticMemberClient struct {
	read func() (nativeauth.MemberDirectory, error)
}

func (c *syntheticMemberClient) Members(context.Context, string) (nativeauth.MemberDirectory, error) {
	return c.read()
}
func (*syntheticMemberClient) Close() {}

func TestPostgresMemberReadsFencesCooldownAndNoRoleGrant(t *testing.T) {
	db := receiverTestDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32)))
	scheduler := NewReadScheduler(ctx)
	defer scheduler.Close()
	manager := NewReceiverManager(db, vault, nil)
	manager.reads = scheduler
	var oldDeployment []byte
	var oldVersion int64
	restoreErr := db.QueryRow(ctx, `SELECT sealed,version FROM native_deployment`).Scan(&oldDeployment, &oldVersion)
	defer func() {
		if restoreErr == nil {
			_, _ = db.Exec(ctx, `UPDATE native_deployment SET sealed=$1,version=$2`, oldDeployment, oldVersion)
		} else {
			_, _ = db.Exec(ctx, `DELETE FROM native_deployment`)
		}
	}()
	seal := func(aad string, value any) []byte {
		raw, _ := json.Marshal(value)
		out, err := vault.Seal(aad, raw)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	deployment := seal("native-deployment:v1", nativeauth.Deployment{})
	if _, err := db.Exec(ctx, `INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,$1,988)
        ON CONFLICT(singleton) DO UPDATE SET sealed=excluded.sealed,version=excluded.version`, deployment); err != nil {
		t.Fatal(err)
	}
	var account string
	if err := db.QueryRow(ctx, `INSERT INTO accounts(name,auth_state) VALUES('synthetic-members','authenticated') RETURNING id::text`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, account)
	saved := savedNativeSession{DeploymentVersion: 988, Session: nativeauth.Session{UID: 7, NIMID: "synthetic-me", JWT: "synthetic-old"}}
	credential := seal("native-session:"+account, saved)
	directory := seal("native-directory:"+account, []nativeauth.DirectoryGroup{{ID: "123", Team: "456"}})
	if _, err := db.Exec(ctx, `UPDATE accounts SET credential_sealed=$2 WHERE id=$1`, account, credential); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO native_directory_snapshots(account_id,payload_sealed,sampled_at) VALUES($1,$2,now())`, account, directory); err != nil {
		t.Fatal(err)
	}
	calls := 0
	fake := &syntheticMemberClient{}
	factory := func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (memberReadClient, error) {
		calls++
		return fake, nil
	}
	fake.read = func() (nativeauth.MemberDirectory, error) {
		return nativeauth.MemberDirectory{Group: "123", Complete: true, MappingComplete: true, Pages: 1,
			Members: []nativeauth.GroupMember{{ID: "7", Transport: "synthetic-me", Role: "admin"}}}, nil
	}
	view, err := manager.members(ctx, account, "123", factory)
	if err != nil || view.SelfRole != "admin" || view.AccountEpoch != 1 || view.Team != "456" {
		t.Fatal(view, err)
	}
	var roles int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM account_groups WHERE account_id=$1`, account).Scan(&roles); err != nil || roles != 0 {
		t.Fatal("read-only roster granted native action permission", err)
	}
	if _, err = manager.members(ctx, account, "999", factory); !errors.Is(err, ErrMemberDirectory) || calls != 1 {
		t.Fatal("arbitrary group reached the native client", err)
	}
	if _, err = db.Exec(ctx, `UPDATE native_directory_snapshots SET sampled_at=now()-interval '11 minutes' WHERE account_id=$1`, account); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.members(ctx, account, "123", factory); !errors.Is(err, ErrMemberDirectory) || calls != 1 {
		t.Fatal("stale directory reached native client", err)
	}
	_, _ = db.Exec(ctx, `UPDATE native_directory_snapshots SET sampled_at=now() WHERE account_id=$1`, account)
	fake.read = func() (nativeauth.MemberDirectory, error) {
		return nativeauth.MemberDirectory{Group: "123", Complete: true, Members: []nativeauth.GroupMember{
			{ID: "7", Transport: "another-identity", Role: "admin"},
		}}, nil
	}
	if result, err := manager.members(ctx, account, "123", factory); err == nil || result.Complete {
		t.Fatal("account self role accepted mismatched transport identity")
	}
	fake.read = func() (nativeauth.MemberDirectory, error) {
		return nativeauth.MemberDirectory{}, &nativeauth.Error{Code: "native_rate_limited", RetryAfter: time.Minute}
	}
	if _, err = manager.members(ctx, account, "123", factory); err == nil {
		t.Fatal("429 lost")
	}
	before := calls
	if _, err = manager.members(ctx, account, "123", factory); err == nil || calls != before {
		t.Fatal("durable cooldown bypassed")
	}
	// New scheduler/process state must still observe the database cooldown.
	second := NewReadScheduler(ctx)
	defer second.Close()
	manager.reads = second
	if _, err = manager.members(ctx, account, "123", factory); err == nil || calls != before {
		t.Fatal("replacement scheduler bypassed cooldown")
	}
	_, _ = db.Exec(ctx, `DELETE FROM account_read_state WHERE account_id=$1`, account)
	third := NewReadScheduler(ctx)
	defer third.Close()
	manager.reads = third
	fake.read = func() (nativeauth.MemberDirectory, error) {
		_, e := db.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, account)
		if e != nil {
			t.Fatal(e)
		}
		return nativeauth.MemberDirectory{Group: "123", Complete: true, Members: []nativeauth.GroupMember{{ID: "7"}}}, nil
	}
	value, err := manager.members(ctx, account, "123", factory)
	if !errors.Is(err, ErrMemberScope) || value.Complete || len(value.Members) != 0 {
		t.Fatal("late member response escaped epoch fence", err)
	}
}
