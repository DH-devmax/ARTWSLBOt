package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/database"
)

type BillingService struct{ DB *database.DB }
type PaymentConfirmation struct {
	TenantID         string `json:"tenant_id"`
	PlanVersionID    string `json:"plan_version_id"`
	IdempotencyKey   string `json:"idempotency_key"`
	PaymentReference string `json:"payment_reference"`
	AmountMicroUSDT  int64  `json:"amount_micro_usdt"`
}
type ConfirmedPeriod struct {
	ID       string    `json:"id"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	State    string    `json:"state"`
}

func (s *BillingService) Confirm(ctx context.Context, a Actor, in PaymentConfirmation) (ConfirmedPeriod, error) {
	var out ConfirmedPeriod
	if a.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	if !ValidID(in.TenantID) || !ValidID(in.PlanVersionID) || len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 100 ||
		len(in.PaymentReference) < 3 || len(in.PaymentReference) > 120 {
		return out, Denied(400, "invalid_payment")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, in.TenantID); err != nil {
		return out, err
	}
	var matches bool
	err = tx.QueryRow(ctx, `SELECT p.id::text,p.starts_at,p.ends_at,p.state,
        o.tenant_id=$2 AND o.plan_version_id=$3 AND o.payment_reference=$4 AND o.amount_micro_usdt=$5
        FROM subscription_orders o JOIN subscription_periods p ON p.order_id=o.id WHERE o.idempotency_key=$1`,
		in.IdempotencyKey, in.TenantID, in.PlanVersionID, in.PaymentReference, in.AmountMicroUSDT).Scan(&out.ID, &out.StartsAt, &out.EndsAt, &out.State, &matches)
	if err == nil {
		if !matches {
			return out, Denied(409, "idempotency_conflict")
		}
		return out, nil
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	var plan string
	var price int64
	var groups, ai, members int
	err = tx.QueryRow(ctx, `SELECT c.plan_id::text,c.price_micro_usdt,p.group_limit,p.ai_group_limit,p.member_limit
        FROM commercial_plans c JOIN plans p ON p.id=c.plan_id WHERE c.id=$1 AND c.enabled`, in.PlanVersionID).Scan(&plan, &price, &groups, &ai, &members)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "plan_not_found")
	}
	if err != nil {
		return out, err
	}
	if price != in.AmountMicroUSDT {
		return out, Denied(400, "payment_amount_mismatch")
	}
	if err = periodCapacity(ctx, tx, in.TenantID, groups, ai, members); err != nil {
		return out, err
	}
	var owner bool
	if err = tx.QueryRow(ctx, `SELECT owner_id IS NOT NULL FROM tenants WHERE id=$1`, in.TenantID).Scan(&owner); err != nil {
		return out, err
	}
	if !owner {
		return out, Denied(409, "tenant_owner_confirmation_required")
	}
	order := NewID()
	out.ID = NewID()
	err = tx.QueryRow(ctx, `SELECT greatest(clock_timestamp(),
        coalesce((SELECT max(ends_at) FROM subscription_periods WHERE tenant_id=$1),'-infinity'),
        coalesce((SELECT expires_at FROM subscriptions WHERE tenant_id=$1 AND enabled),'-infinity'))`, in.TenantID).Scan(&out.StartsAt)
	if err != nil {
		return out, err
	}
	out.EndsAt = out.StartsAt.Add(30 * 24 * time.Hour)
	out.State = "scheduled"
	tag, err := tx.Exec(ctx, `INSERT INTO subscription_orders(id,tenant_id,plan_version_id,idempotency_key,payment_reference,amount_micro_usdt,confirmed_by)
        VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
		order, in.TenantID, in.PlanVersionID, in.IdempotencyKey, in.PaymentReference, price, a.ID)
	if err != nil {
		return out, err
	}
	if tag.RowsAffected() != 1 {
		return out, Denied(409, "payment_already_confirmed")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO subscription_periods(id,tenant_id,order_id,plan_version_id,starts_at,ends_at)
        VALUES($1,$2,$3,$4,$5,$6)`, out.ID, in.TenantID, order, in.PlanVersionID, out.StartsAt, out.EndsAt); err != nil {
		return out, err
	}
	if err = activatePeriod(ctx, tx, in.TenantID); err != nil {
		return out, err
	}
	if err = tx.QueryRow(ctx, `SELECT state FROM subscription_periods WHERE id=$1`, out.ID).Scan(&out.State); err != nil {
		return out, err
	}
	// Never put payment references (potential financial identifiers) in general audit.
	digest := sha256.Sum256([]byte(in.PaymentReference))
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
        VALUES($1,$2,'subscription_payment_confirmed',$3,jsonb_build_object('reference_digest',$4::text))`,
		in.TenantID, a.ID, order, hex.EncodeToString(digest[:])); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return out, nil
}
func periodCapacity(ctx context.Context, tx pgx.Tx, tenant string, groupLimit, aiLimit, memberLimit int) error {
	var groups, ai, members int
	if err := tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE ai_enabled OR ai_requested OR ai_account_id IS NOT NULL),
        (SELECT count(*) FROM users WHERE tenant_id=$1 AND NOT disabled) FROM group_bindings WHERE tenant_id=$1 AND active`,
		tenant).Scan(&groups, &ai, &members); err != nil {
		return err
	}
	if groups > groupLimit || ai > aiLimit || members > memberLimit {
		return Denied(409, "plan_below_current_usage")
	}
	return nil
}
func activatePeriod(ctx context.Context, tx pgx.Tx, tenant string) error {
	expired, err := tx.Exec(ctx, `UPDATE subscription_periods SET state='expired' WHERE tenant_id=$1 AND ends_at<=clock_timestamp() AND state<>'expired'`, tenant)
	if err != nil {
		return err
	}
	if expired.RowsAffected() > 0 {
		if err = fenceBillingTasks(ctx, tx, tenant); err != nil {
			return err
		}
	}
	var id, plan, state string
	var ends time.Time
	var credits int64
	var groups, ai, members int
	err = tx.QueryRow(ctx, `SELECT s.id::text,c.plan_id::text,s.state,s.ends_at,c.credits_micro,p.group_limit,p.ai_group_limit,p.member_limit
        FROM subscription_periods s JOIN commercial_plans c ON c.id=s.plan_version_id JOIN plans p ON p.id=c.plan_id
        WHERE s.tenant_id=$1 AND s.starts_at<=clock_timestamp() AND s.ends_at>clock_timestamp() FOR UPDATE OF s`,
		tenant).Scan(&id, &plan, &state, &ends, &credits, &groups, &ai, &members)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if state == "active" {
		return nil
	}
	if err = periodCapacity(ctx, tx, tenant, groups, ai, members); err != nil {
		_, err = tx.Exec(ctx, `UPDATE subscription_periods SET state='blocked',reason='plan_below_current_usage' WHERE id=$1`, id)
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO subscriptions(tenant_id,plan_id,expires_at,enabled) VALUES($1,$2,$3,true)
        ON CONFLICT(tenant_id) DO UPDATE SET plan_id=excluded.plan_id,expires_at=excluded.expires_at,enabled=true,version=subscriptions.version+1`,
		tenant, plan, ends); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ai_wallets(period_id,tenant_id,granted_micro) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, id, tenant, credits); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO credit_entries(tenant_id,period_id,kind,amount_micro,external_key)
        VALUES($1,$2,'grant',$3,$4) ON CONFLICT DO NOTHING`, tenant, id, credits, "period:"+id); err != nil {
		return err
	}
	if err = fenceBillingTasks(ctx, tx, tenant); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE subscription_periods SET state='active',reason='' WHERE id=$1`, id)
	return err
}
func fenceBillingTasks(ctx context.Context, tx pgx.Tx, tenant string) error {
	if _, err := tx.Exec(ctx, `UPDATE group_bindings SET epoch=epoch+1,version=version+1 WHERE tenant_id=$1`, tenant); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE operations SET status='cancelled',result_code='subscription_period_changed',updated_at=now()
		WHERE tenant_id=$1 AND status='queued'`, tenant); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE assistant_turns SET cancel_requested=true WHERE tenant_id=$1 AND status IN ('queued','running')`, tenant)
	return err
}
func (s *BillingService) Refresh(ctx context.Context, tenant string) error {
	if !ValidID(tenant) {
		return nil
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, tenant); err != nil {
		return err
	}
	if err = activatePeriod(ctx, tx, tenant); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
