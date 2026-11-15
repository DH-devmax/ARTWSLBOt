package control

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// PublishPlan copies an immutable tier snapshot; it never changes existing orders.
func (s *BillingService) PublishPlan(ctx context.Context, a Actor, code string, base int64) (string, error) {
	if a.Role != "superadmin" {
		return "", Denied(403, "superadmin_required")
	}
	if code != "PLUS" && code != "PRO" || base < 1 {
		return "", Denied(400, "invalid_plan")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798461)`); err != nil {
		return "", err
	}
	var prior string
	var version int64
	if err = tx.QueryRow(ctx, `SELECT plan_id::text,version FROM commercial_plans WHERE code=$1 ORDER BY version DESC LIMIT 1`, code).
		Scan(&prior, &version); err != nil {
		return "", err
	}
	if version != base {
		return "", Denied(409, "version_conflict")
	}
	plan, id := NewID(), NewID()
	if _, err = tx.Exec(ctx, `INSERT INTO plans(id,name,group_limit,ai_group_limit,monthly_ai_tokens,monthly_assistant_tokens,modules,allowed_platforms,member_limit)
		SELECT $1,name,group_limit,ai_group_limit,monthly_ai_tokens,monthly_assistant_tokens,modules,allowed_platforms,member_limit
		FROM plans WHERE id=$2`, plan, prior); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO commercial_plans(id,code,version,plan_id,price_micro_usdt,credits_micro)
		SELECT $1,code,version+1,$2,price_micro_usdt,credits_micro FROM commercial_plans WHERE code=$3 AND version=$4`,
		id, plan, code, base); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,resource_id,detail)
		VALUES($1,'commercial_plan_published',$2,jsonb_build_object('code',$3::text,'version',$4::bigint))`,
		a.ID, id, code, version+1); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

type CreditCorrection struct {
	Amount int64  `json:"amount_micro"`
	Key    string `json:"idempotency_key"`
	Reason string `json:"reason"`
}

// Corrections only return a verified overcharge; this is not a credit top-up API.
func (s *GatewayService) CorrectCharge(ctx context.Context, a Actor, call string, in CreditCorrection) error {
	if a.Role != "superadmin" {
		return Denied(403, "superadmin_required")
	}
	if !ValidID(call) || in.Amount <= 0 || len(in.Key) < 8 || len(in.Key) > 100 || len(in.Reason) < 3 || len(in.Reason) > 200 {
		return Denied(400, "invalid_credit_correction")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798460)`); err != nil {
		return err
	}
	key := "correction:" + in.Key
	var matches bool
	err = tx.QueryRow(ctx, `SELECT call_id=$2 AND amount_micro=$3 AND reason=$4 FROM credit_entries WHERE external_key=$1`,
		key, call, in.Amount, in.Reason).Scan(&matches)
	if err == nil {
		if !matches {
			return Denied(409, "idempotency_conflict")
		}
		return nil
	}
	if err != pgx.ErrNoRows {
		return err
	}
	var tenant, period, state string
	var net int64
	err = tx.QueryRow(ctx, `SELECT coalesce(tenant_id::text,''),coalesce(period_id::text,''),status,effective_charged_micro
		FROM ai_call_costs WHERE id=$1`, call).Scan(&tenant, &period, &state, &net)
	if err != nil {
		return err
	}
	if state != "settled" || in.Amount > net {
		return Denied(409, "correction_exceeds_charge")
	}
	if period != "" {
		tag, err := tx.Exec(ctx, `UPDATE ai_wallets SET spent_micro=spent_micro-$2 WHERE period_id=$1 AND spent_micro>=$2`,
			period, in.Amount)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return Denied(409, "ai_reconciliation_budget_conflict")
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO credit_entries(tenant_id,period_id,call_id,actor_id,kind,amount_micro,reason,external_key)
		VALUES(nullif($1,'')::uuid,nullif($2,'')::uuid,$3,$4,'adjustment',$5,$6,$7)`,
		tenant, period, call, a.ID, in.Amount, in.Reason, key); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
		VALUES(nullif($1,'')::uuid,$2,'ai_charge_corrected',$3,jsonb_build_object('amount_micro',$4::bigint))`,
		tenant, a.ID, call, in.Amount); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
