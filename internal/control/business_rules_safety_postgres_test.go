package control

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/security"
)

func (f *businessFixture) ownership(scope BusinessScope) {
	f.t.Helper()
	challenge := NewID()
	f.exec(`INSERT INTO group_binding_challenges(id,tenant_id,applicant_id,platform,account_id,
		account_epoch,account_version,applicant_version,token_hash,private_token_hash,
		create_key,request_hash,state,group_id,proof_event_id,proof_sealed,proof_role)
		VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$1::uuid::text,$9,'bound',$11,1,$12,'owner')`,
		challenge, scope.Tenant, scope.Actor, scope.Platform, scope.Account,
		scope.AccountEpoch, scope.AccountVersion, scope.ActorVersion,
		security.Digest(challenge), security.Digest("private:"+challenge), scope.Group, []byte("synthetic-proof"))
	tx, err := f.db.Platform.Begin(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	event := int64(1)
	bindings := GroupBindingService{DB: f.db, Vault: f.service.Vault}
	err = bindings.saveOwnership(f.ctx, tx,
		bindingRecord{ID: challenge, Group: scope.Group, Tenant: scope.Tenant, Platform: scope.Platform, ProofEvent: &event},
		bindingProof{Event: event, Owner: "native-owner", Sender: "native-owner", SenderRole: "owner", RobotRole: "admin"})
	if err != nil {
		f.t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		f.t.Fatal(err)
	}
}

func TestPostgresBusinessRulesOwnershipFinalFence(t *testing.T) {
	for _, mode := range []string{"manual", "verified", "owner_changed", "missing_anchor"} {
		t.Run(mode, func(t *testing.T) {
			f := newBusinessFixture(t)
			account := f.account("lajiaoliao", 1)
			scope := f.group("lajiaoliao", account, f.tenant, 1, true)
			if mode != "manual" {
				f.ownership(scope)
			}
			job := f.queue(scope, "blocked", "hello", 1)
			if mode == "owner_changed" || mode == "missing_anchor" {
				f.service.Execute = func(ctx context.Context, in BusinessAttempt, fence BusinessFence) (bool, error) {
					proof, err := f.proof(ctx, in.Scope, in.Sender)
					if err != nil {
						return false, err
					}
					if mode == "owner_changed" {
						proof.OwnerID = "different-native-owner"
					} else {
						if _, err = f.db.Platform.Exec(ctx, `DELETE FROM group_binding_ownership WHERE group_id=$1`, scope.Group); err != nil {
							return false, err
						}
					}
					err = fence(ctx, proof, func(context.Context) error { f.writes.Add(1); return nil })
					return err == nil, err
				}
			}
			if err := f.service.execute(f.ctx, job); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "manual":
				if f.writes.Load() != 1 || f.count(`SELECT count(*) FROM group_binding_ownership`) != 0 {
					t.Fatal("manual binding failed or acquired an invented ownership proof", f.state(job))
				}
			case "verified":
				if f.writes.Load() != 1 || !strings.HasPrefix(f.state(job), "succeeded:") {
					t.Fatal("stable ownership anchor rejected a permitted write", f.state(job))
				}
			case "owner_changed":
				if f.writes.Load() != 0 || !strings.HasPrefix(f.state(job), "failed:") ||
					f.count(`SELECT count(*) FROM group_binding_ownership WHERE group_id=$1 AND state='paused'`, scope.Group) != 1 ||
					f.count(`SELECT count(*) FROM group_bindings WHERE group_id=$1 AND NOT management_enabled AND epoch>$2`, scope.Group, scope.BindingEpoch) != 1 ||
					f.count(`SELECT count(*) FROM group_business_runtime WHERE group_id=$1 AND NOT enabled`, scope.Group) != 1 {
					t.Fatal("ownership change escaped the fence or its pause was rolled back", f.state(job))
				}
			case "missing_anchor":
				if f.writes.Load() != 0 || f.state(job) != "failed:group_ownership_proof_required" {
					t.Fatal("lost authenticated proof fell back to a manual binding", f.state(job))
				}
			}
		})
	}
}

func (f *businessFixture) manualOperation(scope BusinessScope, status string, at time.Time) {
	f.t.Helper()
	id := NewID()
	f.exec(`INSERT INTO native_setting_operations(id,actor_id,account_id,group_id,kind,idempotency_key,
		request_hash,account_epoch,status,expires_at,updated_at)
		VALUES($1::uuid,$2,$3,$4,'nickname',$1::uuid::text,$5,$6,$7,clock_timestamp()+interval '1 minute',$8)`,
		id, scope.Actor, scope.Account, scope.Group, bytes.Repeat([]byte{31}, 32), scope.AccountEpoch, status, at)
}

func TestPostgresBusinessRulesManualWriteExclusion(t *testing.T) {
	f := newBusinessFixture(t)
	account := f.account("lajiaoliao", 1)
	scope := f.group("lajiaoliao", account, f.tenant, 1, true)
	for _, phase := range []string{"claim", "final"} {
		for _, state := range []string{"preparing", "submitting", "unknown", "succeeded", "failed", "old_succeeded"} {
			t.Run(phase+"/"+state, func(t *testing.T) {
				f.exec(`DELETE FROM business_rule_jobs; DELETE FROM business_rule_receipts;
					DELETE FROM native_setting_operations; DELETE FROM business_rule_account_cooldowns;
					UPDATE business_rule_consumers SET last_reply_at=NULL`)
				f.roles.Store(0)
				f.writes.Store(0)
				f.attempts.Store(0)
				f.service.Execute = f.execute
				job := f.queue(scope, "human", "hello", 1)
				at := time.Now()
				status := state
				if state == "old_succeeded" {
					status, at = "succeeded", at.Add(-10*time.Second)
				}
				if phase == "claim" {
					f.manualOperation(scope, status, at)
				} else {
					f.service.Execute = func(ctx context.Context, in BusinessAttempt, fence BusinessFence) (bool, error) {
						f.manualOperation(scope, status, at)
						return f.execute(ctx, in, fence)
					}
				}
				if err := f.service.execute(f.ctx, job); err != nil {
					t.Fatal(err)
				}
				if state == "old_succeeded" {
					if f.writes.Load() != 1 || !strings.HasPrefix(f.state(job), "succeeded:") {
						t.Fatal("completed operation beyond five seconds blocked business", f.state(job))
					}
				} else if f.writes.Load() != 0 ||
					f.count(`SELECT count(*) FROM business_rule_attempts WHERE submitted_at IS NOT NULL`) != 0 {
					t.Fatal("pending/unknown/recent manual operation overlapped a business write", f.state(job))
				}
				if phase == "claim" && state != "old_succeeded" && f.attempts.Load() != 0 {
					t.Fatal("account was not rejected before executing the protocol port")
				}
			})
		}
	}
}

func TestPostgresBusinessRulesAccountLockCoordinatesPreparation(t *testing.T) {
	f := newBusinessFixture(t)
	account := f.account("lajiaoliao", 1)
	scope := f.group("lajiaoliao", account, f.tenant, 1, true)
	job := f.queue(scope, "human", "hello", 1)
	lock := func() func() {
		tx, err := f.db.Platform.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(f.ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, account); err != nil {
			tx.Rollback(f.ctx)
			t.Fatal(err)
		}
		return func() { _ = tx.Rollback(f.ctx) }
	}
	release := lock()
	ctx, cancel := context.WithTimeout(f.ctx, 200*time.Millisecond)
	claimed, err := f.service.claim(ctx, job, scope.snapshot())
	cancel()
	release()
	if err == nil || claimed || !strings.HasPrefix(f.state(job), "queued:") {
		t.Fatal("claim passed a competing human preparation account lock")
	}
	claimed, err = f.service.claim(f.ctx, job, scope.snapshot())
	if err != nil || !claimed {
		t.Fatal("released account could not progress", claimed, err)
	}
	_, payload, _, _, err := f.service.loadJob(f.ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := f.proof(f.ctx, scope, payload.Sender)
	if err != nil {
		t.Fatal(err)
	}
	release = lock()
	ctx, cancel = context.WithTimeout(f.ctx, 200*time.Millisecond)
	_, err = f.service.prepareAttempt(ctx, job, scope, payload, proof)
	cancel()
	release()
	if err == nil || f.count(`SELECT count(*) FROM business_rule_attempts`) != 0 {
		t.Fatal("prepare did not share the initial exclusive account lock")
	}
	if _, err = f.service.prepareAttempt(f.ctx, job, scope, payload, proof); err != nil {
		t.Fatal("released account could not prepare", err)
	}
}

func TestPostgresBusinessRulesSlowACKAndActualWriteGap(t *testing.T) {
	f := newBusinessFixture(t)
	account := f.account("lajiaoliao", 1)
	scope := f.group("lajiaoliao", account, f.tenant, 1, true)
	second := f.group("lajiaoliao", account, f.tenant, 2, true)
	job := f.queue(scope, "human", "hello", 1)
	pending := f.queue(second, "human", "hello", 2)
	var ended time.Time
	f.service.Execute = func(ctx context.Context, in BusinessAttempt, fence BusinessFence) (bool, error) {
		proof, err := f.proof(ctx, in.Scope, in.Sender)
		if err != nil {
			return false, err
		}
		err = fence(ctx, proof, func(write context.Context) error {
			f.writes.Add(1)
			renew, cancel := context.WithTimeout(write, 500*time.Millisecond)
			defer cancel()
			// A read-only receiver lease renewal must remain possible while an
			// ACK takes longer than the old two-second submission budget.
			if _, err := f.db.Platform.Exec(renew, `UPDATE account_receivers SET
				lease_until=clock_timestamp()+interval '15 seconds',observed_at=clock_timestamp()
				WHERE account_id=$1`, account); err != nil {
				return err
			}
			timer := time.NewTimer(2200 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-write.Done():
				return write.Err()
			case <-timer.C:
				ended = time.Now()
				return nil
			}
		})
		return err == nil, err
	}
	if err := f.service.execute(f.ctx, job); err != nil {
		t.Fatal(err)
	}
	if f.state(job) != "succeeded:business_native_acknowledged" || f.writes.Load() != 1 || ended.IsZero() {
		t.Fatal("a bounded slow ACK was misclassified as unknown", f.state(job))
	}
	var next time.Time
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT next_write_at FROM business_rule_account_cooldowns
		WHERE account_id=$1`, account).Scan(&next); err != nil || next.Before(ended.Add(businessWriteGap)) {
		t.Fatal("cooldown did not cover at least five seconds after the actual write", next, ended, err)
	}
	if err := f.service.execute(f.ctx, pending); err != nil {
		t.Fatal(err)
	}
	if f.writes.Load() != 1 || !strings.HasPrefix(f.state(pending), "queued:") {
		t.Fatal("another group on the account escaped the persisted write gap", f.state(pending))
	}
}
