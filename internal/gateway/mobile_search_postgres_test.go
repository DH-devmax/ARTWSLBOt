package gateway

import (
	"context"
	"sync"
	"testing"
)

func TestPostgresMobileRequestCountsPersistAndFence(t *testing.T) {
	db := receiverTestDB(t)
	ctx := context.Background()
	var old []byte
	var oldVersion int64
	prior := db.QueryRow(ctx, `SELECT sealed,version FROM native_mobile_deployment WHERE singleton`).Scan(&old, &oldVersion)
	t.Cleanup(func() {
		if prior == nil {
			_, _ = db.Exec(ctx, `UPDATE native_mobile_deployment SET sealed=$1,version=$2 WHERE singleton`, old, oldVersion)
		} else {
			_, _ = db.Exec(ctx, `DELETE FROM native_mobile_deployment`)
		}
	})
	if _, err := db.Exec(ctx, `INSERT INTO native_mobile_deployment(singleton,sealed,version)
		VALUES(true,'synthetic'::bytea,988) ON CONFLICT(singleton) DO UPDATE SET
		sealed=excluded.sealed,version=excluded.version`); err != nil {
		t.Fatal(err)
	}
	var account string
	if err := db.QueryRow(ctx, `INSERT INTO accounts(name,auth_state,native_uid)
		VALUES('synthetic-mobile-count','authenticated','9100123') RETURNING id::text`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, account) })
	if _, err := db.Exec(ctx, `INSERT INTO native_mobile_sessions
		(account_id,native_uid,account_epoch,deployment_version,credential_sealed,state)
		VALUES($1,'9100123',1,988,'synthetic'::bytea,'authenticated')`, account); err != nil {
		t.Fatal(err)
	}
	scope := OnboardingNativeScope{Platform: "wsl", AccountID: account, AccountEpoch: 1, AccountVersion: 1}
	base := SettingSnapshot{Platform: "wsl", SelfID: "9100123", AccountEpoch: 1, AccountVersion: 1,
		MobileSessionVersion: 1, MobileDeploymentVersion: 988}
	manager := NewReceiverManager(db, nil, nil)
	source := manager.mobileRequestCounts(scope, base)
	type result struct {
		value uint64
		err   error
	}
	values := make(chan result, 24)
	var workers sync.WaitGroup
	for range 24 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			value, err := source(ctx)
			values <- result{value, err}
		}()
	}
	workers.Wait()
	close(values)
	seen := map[uint64]bool{}
	for value := range values {
		if value.err != nil || value.value >= 24 || seen[value.value] {
			t.Fatal("request counts repeated or failed under concurrency", value.value, value.err)
		}
		seen[value.value] = true
	}
	restarted := NewReceiverManager(db, nil, nil).mobileRequestCounts(scope, base)
	if value, err := restarted(ctx); err != nil || value != 24 {
		t.Fatal("receiver restart reset the mobile request count", value, err)
	}
	for _, change := range []string{"session", "deployment", "account", "identity", "epoch"} {
		scoped, snapshot := scope, base
		switch change {
		case "session":
			snapshot.MobileSessionVersion++
		case "deployment":
			snapshot.MobileDeploymentVersion++
		case "account":
			snapshot.AccountVersion++
		case "identity":
			snapshot.SelfID = "9100124"
		case "epoch":
			snapshot.AccountEpoch++
		}
		if _, err := manager.mobileRequestCounts(scoped, snapshot)(ctx); err == nil {
			t.Fatal("stale scope allocated a mobile request count", change)
		}
	}
	if _, err := db.Exec(ctx, `UPDATE native_mobile_sessions SET
		state='revoked',credential_sealed=NULL WHERE account_id=$1`, account); err != nil {
		t.Fatal(err)
	}
	if _, err := source(ctx); err == nil {
		t.Fatal("revoked session allocated a mobile request count")
	}
	var count int64
	if err := db.QueryRow(ctx, `SELECT request_count FROM native_mobile_sessions WHERE account_id=$1`,
		account).Scan(&count); err != nil || count != 25 {
		t.Fatal("rejected scopes changed the durable count", count, err)
	}
}
