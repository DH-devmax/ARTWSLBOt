package control

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type assistantPlanQueryTrace struct{ loads atomic.Int32 }

func (trace *assistantPlanQueryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "encode(p.payload_sealed") || strings.Contains(data.SQL, "encode(payload_sealed") {
		trace.loads.Add(1)
	}
	return ctx
}

func (*assistantPlanQueryTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestPostgresAssistantHistoryBatchUsesTwoSealedReadsForTenRounds(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newAssistantOutcomesFixture(t, platform)
			session, err := f.assistant.Session(f.f.ctx, f.ref, f.session, true)
			if err != nil {
				t.Fatal(err)
			}
			config, err := f.assistant.propose(f.f.ctx, f.ref, session, assistantOutcomesConfigCall(t, f))
			if err != nil {
				t.Fatal(err)
			}
			action, err := f.assistant.actionTool(f.f.ctx, f.ref, session, assistantOutcomesMuteCall(t, f))
			if err != nil {
				t.Fatal(err)
			}
			configID := config.Data.(AssistantPlan).ID
			actionID := action.Data.(AssistantActionPlan).ID
			previous := f.assistant.DB.Tenant
			cfg := previous.Config()
			trace := &assistantPlanQueryTrace{}
			cfg.ConnConfig.Tracer = trace
			pool, err := pgxpool.NewWithConfig(f.f.ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			f.assistant.DB.Tenant = pool
			t.Cleanup(func() {
				f.assistant.DB.Tenant = previous
				pool.Close()
			})
			var configIDs, actionIDs []string
			for range AssistantHistoryLimit {
				configIDs = append(configIDs, configID)
				actionIDs = append(actionIDs, actionID)
			}
			ctx, err := f.assistant.batchAssistantPlans(f.f.ctx, session, configIDs, actionIDs)
			if err != nil {
				t.Fatal(err)
			}
			batch := cachedAssistantPlans(ctx, session.Actor.ID, platform)
			if batch == nil || len(batch.configs) != 1 || len(batch.actions) != 1 || trace.loads.Load() != 2 {
				t.Fatal("repeated cards caused repeated sealed-plan reads", trace.loads.Load())
			}
			if cachedAssistantPlans(ctx, NewID(), platform) != nil ||
				cachedAssistantPlans(ctx, session.Actor.ID, "foreign") != nil ||
				cachedAssistantPlans(context.Background(), session.Actor.ID, platform) != nil {
				t.Fatal("request snapshots escaped their authorized scope")
			}
		})
	}
}
