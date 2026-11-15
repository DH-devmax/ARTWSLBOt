package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func (f *businessFixture) commitSource(scope BusinessScope, sender, text string, at time.Time) int64 {
	f.t.Helper()
	tx, err := f.db.Platform.Begin(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	event := f.sourceTx(tx, scope, sender, text, at)
	if err = tx.Commit(f.ctx); err != nil {
		f.t.Fatal(err)
	}
	return event
}

func (f *businessFixture) sourceTx(tx pgx.Tx, scope BusinessScope, sender, text string, at time.Time) int64 {
	f.t.Helper()
	var event int64
	if err := tx.QueryRow(f.ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
		VALUES($1,$2,'group',$3,$4) RETURNING id`, scope.Account, NewID(), at, []byte("synthetic-sealed-source")).Scan(&event); err != nil {
		f.t.Fatal(err)
	}
	message := f.source(scope, sender, text, event)
	raw, _ := json.Marshal(message)
	if _, err := tx.Exec(f.ctx, `INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,
		payload_sealed,codec_version,deployment_version,received_at)
		VALUES($1,$2,'decoded','group',$3,$4,7,1,$5)`,
		event, scope.Account, "synthetic:"+scope.NativeGroup, raw, at); err != nil {
		f.t.Fatal(err)
	}
	return event
}

func TestPostgresBusinessRulesColdPollFindsNewlyEnabledGroup(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newBusinessFixture(t)
			account := f.account(platform, 1)
			scope := f.group(platform, account, f.tenant, 1, false)
			before := f.queries.Load()
			ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
			done := f.service.Run(ctx)
			defer func() { cancel(); <-done }()
			for f.queries.Load() == before && ctx.Err() == nil {
				time.Sleep(10 * time.Millisecond)
			}
			if ctx.Err() != nil || f.reads.Load() != 0 || f.roles.Load() != 0 || f.writes.Load() != 0 {
				t.Fatal("idle consumer did not poll without accessing message/native data")
			}
			f.exec(`UPDATE group_business_runtime SET enabled=true,version=version+1 WHERE group_id=$1`, scope.Group)
			event := f.commitSource(scope, "human", "no match", time.Now())
			for ctx.Err() == nil && f.count(`SELECT count(*) FROM business_rule_receipts
				WHERE group_id=$1 AND event_id=$2`, scope.Group, event) == 0 {
				time.Sleep(20 * time.Millisecond)
			}
			if ctx.Err() != nil || f.reads.Load() != 1 || f.roles.Load() != 0 || f.writes.Load() != 0 ||
				f.count(`SELECT count(*) FROM business_rule_jobs`) != 0 {
				t.Fatal("idle discovery missed a newly enabled group or duplicated its event")
			}
		})
	}
}

func TestPostgresBusinessRulesLateCommitAndDisabled(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newBusinessFixture(t)
			account := f.account(platform, 1)
			scope := f.group(platform, account, f.tenant, 1, false)
			f.commitSource(scope, "human", "hello", time.Now())
			start := f.queries.Load()
			for i := 0; i < 3; i++ {
				groups, err := f.service.groups(f.ctx, "")
				if err != nil || len(groups) != 0 {
					t.Fatal("disabled runtime was discovered", groups, err)
				}
				if f.service.discover(f.ctx, scope.Group, true) == nil {
					t.Fatal("disabled runtime read a source")
				}
			}
			if f.reads.Load() != 0 || f.roles.Load() != 0 || f.writes.Load() != 0 ||
				f.count(`SELECT count(*) FROM business_rule_jobs`) != 0 ||
				f.count(`SELECT count(*) FROM business_rule_consumers`) != 0 {
				t.Fatal("disabled group created work or read native data")
			}
			t.Logf("%s disabled: three polls+rechecks SQL=%d, source/native ports=0, jobs=0",
				platform, f.queries.Load()-start-2)
			f.exec(`UPDATE group_business_runtime SET enabled=true,version=version+1 WHERE group_id=$1`, scope.Group)
			var err error
			scope, _, err = f.service.scope(f.ctx, f.db.Platform, scope.Group)
			if err != nil {
				t.Fatal(err)
			}
			// Allocate a lower ID in an open transaction, then commit a higher
			// ID first. Recovery must find the late commit below its watermark.
			tx, err := f.db.Platform.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(f.ctx)
			lower := f.sourceTx(tx, scope, "human", "no match", time.Now())
			higher := f.commitSource(scope, "human", "no match", time.Now())
			if higher <= lower {
				t.Fatal("fixture did not allocate increasing IDs")
			}
			if err = f.service.discover(f.ctx, scope.Group, false); err != nil {
				t.Fatal(err)
			}
			if f.reads.Load() != 1 || f.roles.Load() != 0 {
				t.Fatal("pre-activation history was read or a nonmatch read roles")
			}
			if err = tx.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			if err = f.service.discover(f.ctx, scope.Group, true); err != nil {
				t.Fatal(err)
			}
			if f.reads.Load() != 2 || f.roles.Load() != 0 ||
				f.count(`SELECT count(*) FROM business_rule_receipts WHERE group_id=$1`, scope.Group) != 2 {
				t.Fatal("late commit was missed or duplicated")
			}
			if err = f.service.discover(f.ctx, scope.Group, true); err != nil {
				t.Fatal(err)
			}
			if f.reads.Load() != 2 {
				t.Fatal("watermark recovery repeatedly decoded old receipts")
			}
			f.exec(`UPDATE group_business_runtime SET enabled=false,version=version+1 WHERE group_id=$1`, scope.Group)
			ctx, cancel := context.WithCancel(f.ctx)
			done := f.service.Run(ctx)
			time.Sleep(100 * time.Millisecond)
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("disabled poller did not cancel")
			}
			if f.service.Snapshot().Active != 0 || f.roles.Load() != 0 ||
				f.count(`SELECT count(*) FROM business_rule_jobs`) != 0 {
				t.Fatal("closed background worker created a task")
			}
		})
	}
}

func TestPostgresBusinessRulesRoleBudgetAndRecovery(t *testing.T) {
	f := newBusinessFixture(t)
	account := f.account("lajiaoliao", 1)
	scope := f.group("lajiaoliao", account, f.tenant, 1, true)
	real := f.service.FreshRole
	f.service.FreshRole = func(ctx context.Context, scope BusinessScope, sender string) (BusinessEvidence, error) {
		proof, err := real(ctx, scope, sender)
		proof.TargetRole = ""
		return proof, err
	}
	batch := make([]BusinessMessage, 100)
	for i := range batch {
		batch[i] = f.source(scope, "human", "hello", int64(i+1))
	}
	if err := f.service.consumeMessages(f.ctx, scope, f.config, batch); err != nil {
		t.Fatal(err)
	}
	if f.roles.Load() != businessRoleReads ||
		f.count(`SELECT count(*) FROM business_rule_receipts WHERE result_code='business_role_read_limit'`) != 98 ||
		f.count(`SELECT count(*) FROM business_rule_jobs`) != 0 {
		t.Fatal("an unresolved role caused unbounded native reads")
	}
	cancelled, cancel := context.WithCancel(f.ctx)
	cancel()
	if f.service.consumeMessages(cancelled, scope, f.config, batch) != context.Canceled {
		t.Fatal("cancelled scanner kept consuming candidates")
	}
	f.service.FreshRole = real
	first := f.queue(scope, "human", "hello", 101)
	f.exec(`UPDATE business_rule_jobs SET status='executing' WHERE id=$1`, first)
	pending := f.queue(scope, "human", "hello", 102)
	if err := f.service.Recover(f.ctx); err != nil {
		t.Fatal("active+pending recovery violated the pending unique index", err)
	}
	if !strings.HasPrefix(f.state(first), "superseded:") || !strings.HasPrefix(f.state(pending), "queued:") {
		t.Fatal("recovery did not keep one unsubmitted pending task", f.state(first), f.state(pending))
	}
}

func TestPostgresBusinessRulesSlowFreshRead(t *testing.T) {
	f := newBusinessFixture(t)
	account := f.account("lajiaoliao", 1)
	scope := f.group("lajiaoliao", account, f.tenant, 1, true)
	f.commitSource(scope, "human", "hello", time.Now())
	read := f.service.FreshRole
	f.service.FreshRole = func(ctx context.Context, scope BusinessScope, target string) (BusinessEvidence, error) {
		timer := time.NewTimer(2200 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return BusinessEvidence{}, ctx.Err()
		case <-timer.C:
			return read(ctx, scope, target)
		}
	}
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	done := f.service.Run(ctx)
	defer func() { cancel(); <-done }()
	until := time.NewTimer(7 * time.Second)
	defer until.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-until.C:
			t.Fatal("a supported native read lasting over two seconds could not execute")
		case <-tick.C:
			if f.count(`SELECT count(*) FROM business_rule_jobs WHERE status='succeeded'`) == 1 {
				if f.writes.Load() != 1 {
					t.Fatal("slow read produced duplicate or missing writes")
				}
				return
			}
		}
	}
}

func TestPostgresBusinessRules100And300GroupsHotspotBounds(t *testing.T) {
	f := newBusinessFixture(t)
	tenants := []string{f.tenant}
	for i := 1; i < 10; i++ {
		tenant, _ := f.customer()
		tenants = append(tenants, tenant)
	}
	var scopes []BusinessScope
	accounts := make(map[int]string)
	for _, size := range []int{100, 300} {
		for i := len(scopes); i < size; i++ {
			pool := i / 30
			platform := "wsl"
			if pool%2 == 1 {
				platform = "lajiaoliao"
			}
			if accounts[pool] == "" {
				accounts[pool] = f.account(platform, pool)
			}
			scopes = append(scopes, f.group(platform, accounts[pool], tenants[i%10], i, true))
		}
		f.exec(`UPDATE account_receivers SET observed_at=clock_timestamp()`)
		f.exec(`DELETE FROM business_rule_jobs; DELETE FROM business_rule_receipts`)
		missSQL, missRoles := f.queries.Load(), f.roles.Load()
		for i := 0; i < size; i++ {
			scope := scopes[i]
			message := f.source(scope, "human", "no literal match", int64(size*3+i+1))
			if err := f.service.consumeMessages(f.ctx, scope, f.config, []BusinessMessage{message}); err != nil {
				t.Fatal(err)
			}
		}
		if f.roles.Load() != missRoles || f.count(`SELECT count(*) FROM business_rule_jobs`) != 0 {
			t.Fatal("nonmatching traffic performed native work or created tasks", size)
		}
		t.Logf("%d groups, one nonmatch/group: SQL requests=%d; native role reads=0; jobs=0",
			size, f.queries.Load()-missSQL-1)
		f.exec(`DELETE FROM business_rule_receipts`)
		start, roleStart := f.queries.Load(), f.roles.Load()
		// Exactly half the input targets one hot group. The other half is
		// interleaved across accounts and platforms instead of draining batches.
		hot := make([]BusinessMessage, size)
		for i := range hot {
			hot[i] = f.source(scopes[0], "human", "hello", int64(i+1))
		}
		if err := f.service.consumeMessages(f.ctx, scopes[0], f.config, hot); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < size; i++ {
			scope := scopes[(i*31)%size]
			message := f.source(scope, "human", "hello", int64(size+i+1))
			if err := f.service.consumeMessages(f.ctx, scope, f.config, []BusinessMessage{message}); err != nil {
				t.Fatal(err)
			}
		}
		sql := f.queries.Load() - start
		if queued := f.count(`SELECT count(*) FROM business_rule_jobs WHERE status='queued'`); queued != businessWaiting {
			t.Fatal("global pending boundary", size, queued)
		}
		if f.roles.Load()-roleStart != businessWaiting {
			t.Fatal("full/hot queues performed additional native role reads", size, f.roles.Load()-roleStart)
		}
		if f.count(`SELECT count(*) FROM (SELECT tenant_id FROM business_rule_jobs WHERE status='queued'
			GROUP BY tenant_id HAVING count(*)>8) overflow`) != 0 ||
			f.count(`SELECT count(*) FROM (SELECT group_id FROM business_rule_jobs WHERE status='queued'
			GROUP BY group_id HAVING count(*)>1) overflow`) != 0 {
			t.Fatal("tenant or group waiting boundary exceeded")
		}
		after := ""
		seen := make(map[string]bool, size)
		pages := f.queries.Load()
		for i := 0; i < size/businessGroupPage; i++ {
			groups, err := f.service.groups(f.ctx, after)
			if err != nil || len(groups) != businessGroupPage {
				t.Fatal("bounded discovery page", len(groups), err)
			}
			for _, group := range groups {
				if seen[group] {
					t.Fatal("group starved during fair discovery rotation")
				}
				seen[group] = true
				after = group
			}
		}
		if len(seen) != size || f.queries.Load()-pages != int64(size/businessGroupPage) {
			t.Fatal("group count increased page size or duplicated scans")
		}
		t.Logf("%d groups; %d events, 50%% hotspot: SQL requests=%d; role reads=%d; pending=64; discovery pages=%d",
			size, 2*size, sql, f.roles.Load()-roleStart, size/businessGroupPage)
	}
	// Four distinct accounts may execute, each tenant at most twice. A fifth
	// attempt cannot reserve an active slot.
	rows, err := f.db.Platform.Query(f.ctx, `SELECT id::text,scope_snapshot FROM (
		SELECT DISTINCT ON(account_id) id,scope_snapshot,account_id FROM business_rule_jobs
		WHERE status='queued' ORDER BY account_id,created_at) candidates LIMIT 5`)
	if err != nil {
		t.Fatal(err)
	}
	type candidate struct {
		id       string
		snapshot []byte
	}
	var claims []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.snapshot); err != nil {
			t.Fatal(err)
		}
		claims = append(claims, c)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	active := 0
	for _, c := range claims {
		ok, err := f.service.claim(f.ctx, c.id, c.snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			active++
		}
	}
	if active > businessWorkers || f.count(`SELECT count(*) FROM business_rule_jobs WHERE status='executing'`) > businessWorkers ||
		f.count(`SELECT count(*) FROM (SELECT tenant_id FROM business_rule_jobs WHERE status='executing'
			GROUP BY tenant_id HAVING count(*)>2) overflow`) != 0 {
		t.Fatal("active concurrency boundary exceeded", active)
	}
	t.Logf("claimed %d/%d account-distinct jobs; global cap=4, tenant cap=2, account serial", active, len(claims))
	if active == 0 {
		t.Fatal("fair queue made no progress")
	}
	var runtimeCount int
	if err = f.db.Platform.QueryRow(f.ctx, `SELECT count(*) FROM group_business_runtime WHERE enabled`).Scan(&runtimeCount); err != nil ||
		runtimeCount != 300 {
		t.Fatal(fmt.Sprint("synthetic scale fixture incomplete: ", runtimeCount), err)
	}
	if f.service.Snapshot().Active != 0 {
		t.Fatal("scale fixture created one goroutine per group")
	}
	f.exec(`DELETE FROM business_rule_jobs; DELETE FROM business_rule_receipts;
		UPDATE group_business_runtime SET enabled=false,version=version+1`)
	readStart, roleStart, writeStart, queryStart := f.reads.Load(), f.roles.Load(), f.writes.Load(), f.queries.Load()
	ctx, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
	done := f.service.Run(ctx)
	<-done
	cancel()
	sql := f.queries.Load() - queryStart
	if f.reads.Load() != readStart || f.roles.Load() != roleStart || f.writes.Load() != writeStart ||
		f.count(`SELECT count(*) FROM business_rule_jobs`) != 0 ||
		f.count(`SELECT count(*) FROM business_rule_receipts`) != 0 || f.service.Snapshot().Active != 0 {
		t.Fatal("300 disabled groups created tasks, bodies, or native work")
	}
	t.Logf("300 disabled groups, one poll: SQL requests=%d; source/native ports=0; jobs=0; workers=0", sql)
}

func TestPostgresBusinessRulesHeatSharesPureDecision(t *testing.T) {
	f := newBusinessFixture(t)
	account := f.account("lajiaoliao", 1)
	scope := f.group("lajiaoliao", account, f.tenant, 1, true)
	chat := HeatScope{Platform: scope.Platform, Tenant: scope.Tenant, Group: scope.Group, NativeGroup: scope.NativeGroup}
	for _, tc := range []struct {
		text string
		want bool
	}{{"hello", true}, {"bad", true}, {"no match", false}, {"群绑定 " + strings.Repeat("a", 43), true}} {
		before := f.roles.Load()
		got, err := f.service.SuppressesHeat(f.ctx, chat, HeatMessage{Group: scope.NativeGroup, Sender: "human", Text: tc.text})
		if err != nil || got != tc.want {
			t.Fatal("heat/worker rule mismatch", tc.text, got, err)
		}
		if !tc.want && f.roles.Load() != before {
			t.Fatal("nonmatch heat message performed a native read")
		}
	}
	f.exec(`UPDATE group_business_runtime SET enabled=false,version=version+1 WHERE group_id=$1`, scope.Group)
	before := f.roles.Load()
	got, err := f.service.SuppressesHeat(f.ctx, chat, HeatMessage{Group: scope.NativeGroup, Sender: "human", Text: "hello bad"})
	if err != nil || got || f.roles.Load() != before ||
		f.count(`SELECT count(*) FROM business_rule_jobs`) != 0 {
		t.Fatal("disabled rules intercepted heat or created native work", err)
	}
}
