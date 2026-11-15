package control

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/nativewire"
)

type TextEvidencePort interface {
	TextEvidence(context.Context, nativewire.TextEvidenceQuery) (nativewire.TextEvidence, error)
}

// Reconcile reads server-held evidence; there is no resend or browser-supplied
// receipt. No evidence, a truncated scan or conflicting evidence stays unknown.
func (s *NativeTextService) Reconcile(ctx context.Context, ref SessionRef, id string, port TextEvidencePort) (TextOperation, error) {
	out := TextOperation{ID: id}
	if !ValidID(id) || port == nil {
		return out, Denied(400, "invalid_operation")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	actor, err := sessionActor(ctx, tx, ref, false)
	if err != nil {
		return out, err
	}
	if actor.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	var query nativewire.TextEvidenceQuery
	var tenant string
	err = tx.QueryRow(ctx, `SELECT o.status,o.result_code,o.tenant_id::text,o.account_id::text,
        o.account_epoch,t.client_id::text,t.submitted_at,r.team,r.sender,r.deployment_version,
        r.attachment_digest,r.observed_at,r.private_peer FROM operations o
        JOIN native_text_attempts t ON t.operation_id=o.id JOIN native_text_routes r ON r.operation_id=o.id
        WHERE o.id=$1 AND o.actor_id=$2
        AND o.kind IN('send_text','send_image','send_voice','send_video','send_file')`, id, actor.ID).
		Scan(&out.Status, &out.ResultCode, &tenant, &query.AccountID, &query.AccountEpoch,
			&query.ClientID, &query.SubmittedAt, &query.Route.Team, &query.Route.Sender,
			&query.Route.DeploymentVersion, &query.Route.Digest, &query.Route.ObservedAt, &query.Route.PrivatePeer)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "native_text_evidence_not_found")
	}
	if err != nil {
		return out, err
	}
	if out.Status != "unknown" {
		return out, nil
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	evidence, err := port.TextEvidence(ctx, query)
	if err != nil {
		return out, err
	}
	if !evidence.Found || evidence.Truncated {
		return out, nil
	}
	if evidence.EventID < 1 || evidence.Receipt.ClientID != query.ClientID || !validReceipt(evidence.Receipt) {
		return out, Denied(409, "native_text_evidence_conflict")
	}
	final, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer final.Rollback(ctx)
	if _, err = final.Exec(ctx, `SET LOCAL lock_timeout='750ms'`); err != nil {
		return out, err
	}
	current, err := sessionActor(ctx, final, ref, true)
	if err != nil {
		return out, err
	}
	if current.ID != actor.ID || current.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	var account string
	err = final.QueryRow(ctx, `SELECT id::text FROM accounts WHERE id=$1 AND epoch=$2 FOR SHARE`,
		query.AccountID, query.AccountEpoch).Scan(&account)
	if err != nil {
		return out, Denied(409, "native_text_account_changed")
	}
	err = final.QueryRow(ctx, `SELECT status,result_code FROM operations WHERE id=$1 AND actor_id=$2 FOR UPDATE`,
		id, actor.ID).Scan(&out.Status, &out.ResultCode)
	if err != nil {
		return out, err
	}
	if out.Status != "unknown" {
		return out, nil
	}
	resultCode := "im_server_accepted_reconciled"
	if _, err = final.Exec(ctx, `UPDATE operations SET status=$2,result_code=$3,updated_at=clock_timestamp()
        WHERE id=$1`, id, "succeeded", resultCode); err != nil {
		return out, err
	}
	if _, err = final.Exec(ctx, `UPDATE native_text_attempts SET server_id=$2::numeric,
        server_time_ms=$3::numeric,finished_at=clock_timestamp() WHERE operation_id=$1`,
		id, evidence.Receipt.ServerID, evidence.Receipt.TimeMS); err != nil {
		return out, err
	}
	if _, err = final.Exec(ctx, `UPDATE native_text_routes SET reconciled_event_id=$2 WHERE operation_id=$1`,
		id, evidence.EventID); err != nil {
		return out, err
	}
	auditTenant := any(tenant)
	if query.Route.PrivatePeer != "" {
		auditTenant = nil
	}
	if _, err = final.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
        VALUES($1,$2,'native_text_reconciled',$3,jsonb_build_object('result_code',$4::text,'event_id',$5::bigint))`,
		auditTenant, actor.ID, id, resultCode, evidence.EventID); err != nil {
		return out, err
	}
	if err = final.Commit(ctx); err != nil {
		return TextOperation{ID: id, Status: "unknown", ResultCode: "native_text_reconciliation_required"}, err
	}
	out.Status, out.ResultCode = "succeeded", resultCode
	return out, nil
}
