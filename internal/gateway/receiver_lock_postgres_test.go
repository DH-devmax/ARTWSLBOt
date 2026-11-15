package gateway

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestPostgresReceiverReconcileSkipsSubmissionLocks(t *testing.T) {
	db := receiverTestDB(t)
	for _, test := range []struct {
		name, locks string
		finished    bool
	}{{"renew", "a,r", false}, {"release", "a,r", true},
		{"renew_account_only", "a", false}, {"release_account_only", "a", true}} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			if _, err := db.Exec(ctx, `UPDATE account_receivers SET enabled=false`); err != nil {
				t.Fatal(err)
			}
			manager := NewReceiverManager(db, nil, nil)
			makeWorker := func(uid string) (string, *receiverWorker, *atomic.Bool) {
				t.Helper()
				var account string
				if err := db.QueryRow(ctx, `INSERT INTO accounts(name,platform,native_uid,auth_state,credential_sealed)
					VALUES('synthetic-receiver-lock','lajiaoliao',$1,'authenticated',$2)
					RETURNING id::text`, uid, []byte("synthetic")).Scan(&account); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _, _ = db.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, account) })
				var lease time.Time
				if err := db.QueryRow(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until)
					VALUES($1,true,$2,clock_timestamp()+interval '1 minute') RETURNING lease_until`,
					account, manager.owner).Scan(&lease); err != nil {
					t.Fatal(err)
				}
				now := time.Now()
				connection := &ownedChiliConnection{ctx: ctx, uid: uid,
					transport: &chiliMediaOwnerTransport{chiliObservationTransport{
						done: make(chan struct{}), hints: make(chan struct{}, 1),
					}}}
				cancelled := &atomic.Bool{}
				worker := &receiverWorker{
					spec: receiverSpec{ID: account, UID: uid, Platform: "lajiaoliao", Epoch: 1, Revision: 1},
					done: make(chan struct{}), cancel: func() { cancelled.Store(true) }, chiliConnection: connection,
					leaseUntil: lease, snapshot: ReceiverSnapshot{State: "connected", HeartbeatAt: &now},
				}
				manager.jobs[account] = worker
				return account, worker, cancelled
			}
			account, worker, cancelled := makeWorker("synthetic-lock-self")
			if test.finished {
				close(worker.done)
				if _, err := db.Exec(ctx, `UPDATE account_receivers SET enabled=false WHERE account_id=$1`, account); err != nil {
					t.Fatal(err)
				}
			}
			other, otherWorker, otherCancelled := makeWorker("synthetic-lock-other")
			oldLease := worker.leaseUntil
			owner, err := pgx.ConnectConfig(ctx, db.Config().ConnConfig.Copy())
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close(context.Background())
			locked, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer locked.Rollback(context.Background())
			var lockedID string
			if err = locked.QueryRow(ctx, `SELECT a.id::text FROM accounts a
				JOIN account_receivers r ON r.account_id=a.id WHERE a.id=$1 FOR SHARE OF `+test.locks, account).
				Scan(&lockedID); err != nil {
				t.Fatal(err)
			}
			for round := 0; round < 2; round++ {
				start := time.Now()
				if err = manager.reconcile(ctx, owner); err != nil {
					t.Fatal("submission lock stopped the coordinator:", err)
				}
				if time.Since(start) > time.Second {
					t.Fatal("heartbeat waited on submission rows")
				}
			}
			if (test.locks == "a,r" && worker.leaseUntil != oldLease) ||
				(!test.finished && cancelled.Load()) || otherCancelled.Load() {
				t.Fatal("locked account was renewed or an existing owner was cancelled")
			}
			if _, _, err = manager.chiliMediaOwner(ChiliMediaIntent{
				AccountID: other, ReceiverOwner: manager.owner, Sender: otherWorker.spec.UID,
				AccountEpoch: 1, Revision: 1,
			}); err != nil {
				t.Fatal("unlocked account lost its current owner:", err)
			}
			var savedState string
			if err = db.QueryRow(ctx, `SELECT runtime_state FROM accounts WHERE id=$1`, other).Scan(&savedState); err != nil ||
				savedState != "connected" {
				t.Fatal("unlocked account did not progress:", savedState, err)
			}
			if test.finished {
				if manager.jobs[account] != worker {
					t.Fatal("unfinished lease release was forgotten")
				}
			} else if _, _, err = manager.chiliMediaOwner(ChiliMediaIntent{
				AccountID: account, ReceiverOwner: manager.owner, Sender: worker.spec.UID,
				AccountEpoch: 1, Revision: 1,
			}); err != nil {
				t.Fatal("submission lost its still-current owner:", err)
			}
			if err = locked.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if err = manager.reconcile(ctx, owner); err != nil {
				t.Fatal(err)
			}
			if test.finished {
				if manager.jobs[account] == worker {
					t.Fatal("finished worker was not released after unlocking")
				}
			} else {
				var lease time.Time
				if err = db.QueryRow(ctx, `SELECT lease_until FROM account_receivers WHERE account_id=$1`, account).
					Scan(&lease); err != nil || !worker.leaseUntil.Equal(lease) || worker.leaseUntil == oldLease {
					t.Fatal("renewal did not use its committed database deadline:", err)
				}
			}
		})
	}
}

func TestPostgresReceiverLockedLeaseCannotOutliveAuthority(t *testing.T) {
	db := receiverTestDB(t)
	for _, invalid := range []string{"expired_local", "expired_database", "owner_changed", "epoch_changed", "disabled"} {
		t.Run(invalid, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := db.Exec(ctx, `UPDATE account_receivers SET enabled=false`); err != nil {
				t.Fatal(err)
			}
			manager := NewReceiverManager(db, nil, nil)
			var account string
			if err := db.QueryRow(ctx, `INSERT INTO accounts(name,platform,native_uid,auth_state,credential_sealed)
				VALUES('synthetic-expired-lock','lajiaoliao','synthetic-expired-lock','authenticated',$1)
				RETURNING id::text`, []byte("synthetic")).Scan(&account); err != nil {
				t.Fatal(err)
			}
			defer db.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, account)
			if _, err := db.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until)
				VALUES($1,true,$2,clock_timestamp()+interval '1 minute')`, account, manager.owner); err != nil {
				t.Fatal(err)
			}
			cancelled := &atomic.Bool{}
			worker := &receiverWorker{
				spec: receiverSpec{ID: account, UID: "synthetic-expired-lock", Platform: "lajiaoliao", Epoch: 1, Revision: 1},
				done: make(chan struct{}), cancel: func() { cancelled.Store(true) }, leaseUntil: time.Now().Add(time.Minute),
			}
			manager.jobs[account] = worker
			statement := ""
			switch invalid {
			case "expired_local":
				worker.leaseUntil = time.Now().Add(-time.Second)
			case "expired_database":
				statement = `UPDATE account_receivers SET lease_until=clock_timestamp()-interval '1 second' WHERE account_id=$1`
			case "owner_changed":
				statement = `UPDATE account_receivers SET owner='another-synthetic-owner' WHERE account_id=$1`
			case "epoch_changed":
				statement = `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`
			case "disabled":
				statement = `UPDATE account_receivers SET enabled=false WHERE account_id=$1`
			}
			if statement != "" {
				if _, err := db.Exec(ctx, statement, account); err != nil {
					t.Fatal(err)
				}
			}
			owner, err := pgx.ConnectConfig(ctx, db.Config().ConnConfig.Copy())
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close(context.Background())
			locked, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer locked.Rollback(context.Background())
			var id string
			if err = locked.QueryRow(ctx, `SELECT account_id::text FROM account_receivers
				WHERE account_id=$1 FOR SHARE`, account).Scan(&id); err != nil {
				t.Fatal(err)
			}
			if err = manager.reconcile(ctx, owner); err != nil {
				t.Fatal("one account's stale lease stopped all accounts:", err)
			}
			if !cancelled.Load() {
				t.Fatal("worker retained expired or revoked authority")
			}
		})
	}
}
