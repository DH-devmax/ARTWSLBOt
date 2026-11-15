package control

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestPostgresBusinessRulesRun100And300GroupsLateCommit(t *testing.T) {
	for _, size := range []int{100, 300} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			f := newBusinessFixture(t)
			scopes := make([]BusinessScope, 0, size)
			for i := 0; i < size; {
				platform := "wsl"
				if i/30%2 == 1 {
					platform = "lajiaoliao"
				}
				account := f.account(platform, i/30)
				for end := min(i+30, size); i < end; i++ {
					scopes = append(scopes, f.group(platform, account, f.tenant, i, true))
				}
			}
			late, err := f.db.Platform.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer late.Rollback(f.ctx)
			for _, scope := range scopes {
				f.sourceTx(late, scope, "human", "no literal match", time.Now())
			}
			for i, scope := range scopes {
				if i%businessGroupPage == 0 {
					f.exec(`UPDATE account_receivers SET observed_at=clock_timestamp()`)
				}
				f.commitSource(scope, "human", "no literal match", time.Now())
				if err = f.service.discover(f.ctx, scope.Group, false); err != nil {
					t.Fatal("initial committed watermark", i, err)
				}
			}
			if f.count(`SELECT count(*) FROM business_rule_receipts`) != size {
				t.Fatal("uncommitted sources were consumed")
			}
			if err = late.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			// Start a fresh service with no per-group process state. Run must
			// rotate actual SQL pages and recover every lower committed ID,
			// without the test forcing discover(..., true).
			service := &BusinessRulesService{DB: f.db, Vault: f.service.Vault, Catalog: f.service.Catalog,
				Conversation: f.service.Conversation, ReadSource: f.service.ReadSource,
				FreshRole: f.service.FreshRole, Execute: f.service.Execute}
			f.exec(`UPDATE account_receivers SET observed_at=clock_timestamp()`)
			timeout := time.Duration(2*size/businessGroupPage+10) * time.Second
			ctx, cancel := context.WithTimeout(f.ctx, timeout)
			done := service.Run(ctx)
			defer func() { cancel(); <-done }()
			tick := time.NewTicker(200 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					t.Fatalf("%d-group Run missed late commits: receipts=%d/%d",
						size, f.count(`SELECT count(*) FROM business_rule_receipts`), 2*size)
				case <-tick.C:
					// Synthetic receiver renewal keeps this scheduling test
					// independent of the real native network and wall-clock IO.
					f.exec(`UPDATE account_receivers SET observed_at=clock_timestamp()`)
					if f.count(`SELECT count(*) FROM business_rule_receipts`) != 2*size {
						continue
					}
					cancel()
					<-done
					if f.count(`SELECT count(*) FROM (SELECT group_id FROM business_rule_receipts
						GROUP BY group_id HAVING count(*)<>2) invalid`) != 0 {
						t.Fatal("a group was skipped or consumed twice")
					}
					if f.reads.Load() != int32(2*size) || f.roles.Load() != 0 || f.writes.Load() != 0 ||
						f.count(`SELECT count(*) FROM business_rule_jobs`) != 0 || service.Snapshot().Active != 0 ||
						f.count(`SELECT count(*) FROM business_rule_consumers WHERE last_recovery_at IS NULL`) != 0 {
						t.Fatal("late recovery decoded receipts twice or exceeded the no-match boundary")
					}
					t.Logf("%d groups: actual Run pagination recovered all late commits; source reads=%d; native=0; jobs=0",
						size, f.reads.Load())
					return
				}
			}
		})
	}
}
