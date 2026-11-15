package control

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type BusinessOperation struct {
	ID        string    `json:"id"`
	Platform  string    `json:"platform"`
	Source    string    `json:"source"`
	Rule      string    `json:"rule"`
	Action    string    `json:"kind"`
	Status    string    `json:"status"`
	Code      string    `json:"result_code"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *BusinessRulesService) Operations(ctx context.Context, actor Actor, group string) ([]BusinessOperation, error) {
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = s.readAccess(ctx, tx, actor, group); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id::text,platform,rule,action,status,result_code,created_at
		FROM business_rule_jobs WHERE group_id=$1 ORDER BY created_at DESC,id DESC LIMIT 50`, group)
	if err != nil {
		return nil, err
	}
	out := make([]BusinessOperation, 0, 50)
	for rows.Next() {
		var item BusinessOperation
		item.Source = "automatic"
		if err = rows.Scan(&item.ID, &item.Platform, &item.Rule, &item.Action, &item.Status, &item.Code, &item.CreatedAt); err != nil {
			break
		}
		out = append(out, item)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Access is checked after materializing the bounded metadata projection too.
	if err = s.readAccess(ctx, tx, actor, group); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func (s *BusinessRulesService) readAccess(ctx context.Context, tx pgx.Tx, actor Actor, group string) error {
	grants, err := LiveActorPermissions(ctx, tx, actor, s.Catalog)
	if err != nil {
		return err
	}
	allowed := actor.Role == "superadmin"
	for _, grant := range grants {
		allowed = allowed || grant == "operations:read"
	}
	if !allowed {
		return Denied(403, "permission_denied")
	}
	return RequireGroup(ctx, tx, actor, group)
}

// Reconcile observes the original attempt only. It never calls Execute, obtains
// another nonce, changes the task's immutable scope or releases its dedupe key.
func (s *BusinessRulesService) Reconcile(ctx context.Context, actor Actor, group, id string) error {
	if !ValidID(id) || !ValidID(group) {
		return Denied(404, "operation_not_found")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = s.readAccess(ctx, tx, actor, group); err != nil {
		return err
	}
	var exists bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM business_rule_jobs
		WHERE id=$1 AND group_id=$2 AND status='unknown')`, id, group).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return Denied(409, "business_operation_not_unknown")
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	scope, payload, _, status, err := s.loadJob(ctx, id)
	if err != nil {
		return err
	}
	if status != "unknown" || scope.Group != group {
		return Denied(409, "business_operation_not_unknown")
	}
	if s.Observe == nil {
		return Denied(409, "business_observation_unavailable")
	}
	intent := BusinessAttempt{JobID: id, Scope: scope, Action: payload.Decision.Action, Sender: payload.Sender,
		Target: payload.Decision.Target, Text: payload.Decision.Text, Minutes: payload.Decision.Minutes, Reference: payload.Reference}
	var nonce, aad string
	err = s.DB.Platform.QueryRow(ctx, `SELECT client_id::text,receiver_owner,receiver_revision,
		coalesce(nonce::text,'0'),coalesce(aad::text,'0'),message_at
		FROM business_rule_attempts WHERE job_id=$1`, id).Scan(&intent.ClientID, &intent.Owner,
		&intent.Revision, &nonce, &aad, &intent.MessageAt)
	if err != nil {
		return err
	}
	if intent.Nonce, err = strconv.ParseUint(nonce, 10, 64); err != nil {
		return err
	}
	if intent.AAD, err = strconv.ParseUint(aad, 10, 64); err != nil {
		return err
	}
	var sameIdentity bool
	err = s.DB.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts
		WHERE id=$1 AND platform=$2 AND purpose='management' AND native_uid=$3
		AND epoch=$4 AND auth_state='authenticated')`, scope.Account, scope.Platform, scope.NativeSelf, scope.AccountEpoch).Scan(&sameIdentity)
	if err != nil {
		return err
	}
	if !sameIdentity {
		return Denied(409, "business_account_changed")
	}
	observed, err := s.Observe(ctx, intent)
	if err != nil {
		return err
	}
	if !observed {
		return Denied(409, "business_write_unconfirmed")
	}
	tx, err = s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = s.readAccess(ctx, tx, actor, group); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE business_rule_jobs SET status='succeeded',
		result_code='business_original_effect_observed',updated_at=clock_timestamp()
		WHERE id=$1 AND group_id=$2 AND status='unknown' AND EXISTS(SELECT 1 FROM accounts
			WHERE id=business_rule_jobs.account_id AND epoch=$3 AND native_uid=$4 AND platform=$5)`,
		id, group, scope.AccountEpoch, scope.NativeSelf, scope.Platform)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		_, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
			VALUES($1,$2,'automatic_business_reconciled',$3,jsonb_build_object('job_id',$4::text,'source','automatic'))`,
			scope.Tenant, actor.ID, group, id)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *BusinessRulesService) Prune(ctx context.Context) error {
	_, err := s.DB.Platform.Exec(ctx, `DELETE FROM business_rule_receipts WHERE (group_id,account_id,event_id) IN
		(SELECT group_id,account_id,event_id FROM business_rule_receipts
			WHERE created_at<clock_timestamp()-interval '7 days' ORDER BY created_at LIMIT 512);
		DELETE FROM business_rule_member_cooldowns WHERE (group_id,account_id,member_id) IN
		(SELECT group_id,account_id,member_id FROM business_rule_member_cooldowns
			WHERE until_at<clock_timestamp()-interval '1 day' ORDER BY until_at LIMIT 512);
		UPDATE business_rule_jobs SET scope_sealed=NULL,payload_sealed=NULL WHERE id IN
		(SELECT id FROM business_rule_jobs WHERE created_at<clock_timestamp()-interval '7 days'
			AND (scope_sealed IS NOT NULL OR payload_sealed IS NOT NULL) ORDER BY created_at LIMIT 512)`)
	return err
}
