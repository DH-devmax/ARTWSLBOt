package control

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/features"
	"wangshangliao/internal/nativewire"
)

func (s *BusinessRulesService) dispatchable(ctx context.Context) ([]string, error) {
	if _, err := s.DB.Platform.Exec(ctx, `UPDATE business_rule_jobs SET status='expired',
		result_code='business_task_expired',updated_at=clock_timestamp()
		WHERE status='queued' AND expires_at<=clock_timestamp()`); err != nil {
		return nil, err
	}
	rows, err := s.DB.Platform.Query(ctx, `WITH eligible AS (
		SELECT j.id,j.priority,j.created_at,
			row_number() OVER(PARTITION BY j.tenant_id ORDER BY j.priority,j.created_at,j.id) AS tenant_rank
		FROM business_rule_jobs j WHERE j.status='queued' AND j.expires_at>clock_timestamp()+interval '3 seconds'
		AND NOT EXISTS(SELECT 1 FROM business_rule_jobs busy WHERE busy.account_id=j.account_id
			AND busy.status IN('executing','submitting','unknown'))
		AND (SELECT count(*) FROM business_rule_jobs busy WHERE busy.tenant_id=j.tenant_id
			AND busy.status IN('executing','submitting'))<$1
		AND NOT EXISTS(SELECT 1 FROM business_rule_account_cooldowns c
			WHERE c.account_id=j.account_id AND c.next_write_at>clock_timestamp()))
		SELECT id::text FROM eligible ORDER BY tenant_rank,priority,created_at,id LIMIT $2`,
		businessTenantWork, businessWorkers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0, businessWorkers)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *BusinessRulesService) claim(ctx context.Context, id string, expected []byte) (bool, error) {
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='500ms'`); err != nil {
		return false, err
	}
	// Use the same initial account lock as human-confirmed settings. Claim and
	// their initial pending check cannot both admit an overlapping operation.
	var account string
	if err = tx.QueryRow(ctx, `SELECT account_id::text FROM business_rule_jobs WHERE id=$1`, id).Scan(&account); err != nil {
		return false, err
	}
	if err = tx.QueryRow(ctx, `SELECT id::text FROM accounts WHERE id=$1 FOR UPDATE`, account).Scan(&account); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798504)`); err != nil {
		return false, err
	}
	var group, tenant string
	var snapshot []byte
	err = tx.QueryRow(ctx, `SELECT group_id::text,tenant_id::text,account_id::text,scope_snapshot
		FROM business_rule_jobs WHERE id=$1 AND status='queued'
		AND expires_at>clock_timestamp()+interval '3 seconds' FOR UPDATE`, id).Scan(&group, &tenant, &account, &snapshot)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !bytes.Equal(snapshot, expected) {
		return false, Denied(409, "business_scope_changed")
	}
	var busy bool
	err = tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM business_rule_jobs WHERE status IN('executing','submitting'))>=$1
		OR (SELECT count(*) FROM business_rule_jobs WHERE tenant_id=$2 AND status IN('executing','submitting'))>=$3
		OR EXISTS(SELECT 1 FROM business_rule_jobs WHERE (group_id=$4 OR account_id=$5)
			AND status IN('executing','submitting','unknown'))
		OR EXISTS(SELECT 1 FROM business_rule_account_cooldowns WHERE account_id=$5 AND next_write_at>clock_timestamp())
		OR EXISTS(SELECT 1 FROM native_setting_operations WHERE account_id=$5
			AND (status IN('preparing','submitting','unknown') OR updated_at>clock_timestamp()-interval '5 seconds'))
		OR EXISTS(SELECT 1 FROM operations WHERE account_id=$5 AND status IN('running','unknown'))`,
		businessWorkers, tenant, businessTenantWork, group, account).Scan(&busy)
	if err != nil || busy {
		return false, err
	}
	tag, err := tx.Exec(ctx, `UPDATE business_rule_jobs SET status='executing',updated_at=clock_timestamp()
		WHERE id=$1 AND status='queued'`, id)
	if err != nil || tag.RowsAffected() != 1 {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (s *BusinessRulesService) loadJob(ctx context.Context, id string) (BusinessScope, businessPayload, time.Time, string, error) {
	var scope BusinessScope
	var payload businessPayload
	var sealedScope, sealedPayload, snapshot []byte
	var expiry time.Time
	var status string
	err := s.DB.Platform.QueryRow(ctx, `SELECT scope_sealed,payload_sealed,scope_snapshot,expires_at,status
		FROM business_rule_jobs WHERE id=$1`, id).Scan(&sealedScope, &sealedPayload, &snapshot, &expiry, &status)
	if err != nil || len(sealedScope) == 0 || len(sealedPayload) == 0 {
		return scope, payload, expiry, status, Denied(409, "business_task_unavailable")
	}
	raw, err := s.Vault.Open("business-rule-scope:"+id, sealedScope)
	if err != nil {
		return scope, payload, expiry, status, err
	}
	err = json.Unmarshal(raw, &scope)
	clear(raw)
	if err != nil || !bytes.Equal(scope.snapshot(), snapshot) {
		return scope, payload, expiry, status, Denied(409, "business_scope_changed")
	}
	raw, err = s.Vault.Open("business-rule-payload:"+id, sealedPayload)
	if err != nil {
		return scope, payload, expiry, status, err
	}
	err = json.Unmarshal(raw, &payload)
	clear(raw)
	if err != nil {
		return scope, payload, expiry, status, Denied(409, "business_task_unreadable")
	}
	return scope, payload, expiry, status, nil
}

func (s *BusinessRulesService) execute(ctx context.Context, id string) (result error) {
	defer func() {
		if result != nil {
			work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			_, _ = s.DB.Platform.Exec(work, `UPDATE business_rule_jobs SET
				status=CASE WHEN status='submitting' THEN 'unknown' ELSE 'failed' END,
				result_code='business_work_unconfirmed',updated_at=clock_timestamp()
				WHERE id=$1 AND status IN('executing','submitting')`, id)
		}
	}()
	if s.Execute == nil || s.FreshRole == nil || s.Vault == nil {
		return Denied(503, "business_runtime_unavailable")
	}
	scope, payload, expiry, status, err := s.loadJob(ctx, id)
	if err != nil || status != "queued" {
		return err
	}
	current, config, err := s.scope(ctx, s.DB.Platform, scope.Group)
	if err != nil || !bytes.Equal(current.snapshot(), scope.snapshot()) {
		return s.finish(ctx, id, "cancelled", "business_scope_changed")
	}
	if err = s.authorizeAction(ctx, s.DB.Platform, current, config, payload.Decision.Rule, payload.Decision.Action); err != nil {
		return s.finish(ctx, id, "cancelled", businessErrorCode(err))
	}
	claimed, err := s.claim(ctx, id, scope.snapshot())
	if err != nil || !claimed {
		return err
	}
	work, cancel := context.WithDeadline(ctx, minHeatDeadline(expiry, time.Now().Add(30*time.Second)))
	defer cancel()
	evidence, err := s.FreshRole(work, scope, payload.Sender)
	if err != nil {
		return s.finish(ctx, id, "failed", "business_native_role_unknown")
	}
	if err = validateBusinessEvidence(scope, payload, config, evidence, time.Now()); err != nil {
		return s.finish(ctx, id, "cancelled", businessErrorCode(err))
	}
	intent, err := s.prepareAttempt(work, id, scope, payload, evidence)
	if err != nil {
		return s.finish(ctx, id, "cancelled", businessErrorCode(err))
	}
	var called, attempted atomic.Bool
	fence := func(call context.Context, proof BusinessEvidence, write func(context.Context) error) error {
		if write == nil || !called.CompareAndSwap(false, true) {
			return Denied(409, "business_write_already_attempted")
		}
		final, stop := context.WithTimeout(call, businessWriteFence)
		defer stop()
		if err := s.observeOwner(final, scope, proof); err != nil {
			return err
		}
		if err := s.reserveWrite(final, intent, payload, proof); err != nil {
			return err
		}
		tx, err := s.DB.Platform.Begin(final)
		if err != nil {
			return err
		}
		defer tx.Rollback(context.WithoutCancel(final))
		if err = s.lockScope(final, tx, scope, payload, proof); err != nil {
			return err
		}
		if err = s.lockAttempt(final, tx, intent, true); err != nil {
			return err
		}
		if err = scope.controlledExecutionFence.requireSubmission(final, tx, businessWriteFence); err != nil {
			return err
		}
		attempted.Store(true)
		if err = write(final); err != nil {
			return err
		}
		return tx.Commit(final)
	}
	confirmed, writeErr := s.Execute(work, intent, fence)
	state, code := "failed", "business_write_not_submitted"
	if attempted.Load() {
		state, code = "unknown", "business_write_unconfirmed"
		if writeErr == nil && confirmed {
			state, code = "succeeded", "business_native_acknowledged"
		}
	} else if writeErr != nil {
		code = businessErrorCode(writeErr)
	}
	if state == "succeeded" {
		s.writes.Add(1)
	} else if state == "unknown" {
		s.unknown.Add(1)
	}
	return s.finish(ctx, id, state, code)
}

func (s *BusinessRulesService) prepareAttempt(ctx context.Context, id string, scope BusinessScope,
	payload businessPayload, proof BusinessEvidence) (BusinessAttempt, error) {
	out := BusinessAttempt{JobID: id, ClientID: NewID(), Scope: scope, Action: payload.Decision.Action,
		Sender: payload.Sender, Target: payload.Decision.Target, Text: payload.Decision.Text,
		Minutes: payload.Decision.Minutes, Reference: payload.Reference}
	if err := s.observeOwner(ctx, scope, proof); err != nil {
		return out, err
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='500ms'`); err != nil {
		return out, err
	}
	var account string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM accounts WHERE id=$1 FOR UPDATE`, scope.Account).Scan(&account); err != nil {
		return out, err
	}
	if err = s.lockScope(ctx, tx, scope, payload, proof); err != nil {
		return out, err
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM business_rule_jobs WHERE id=$1
		AND expires_at>clock_timestamp()+interval '3 seconds' FOR UPDATE`, id).Scan(&status); err != nil || status != "executing" {
		return out, Denied(409, "business_task_changed")
	}
	err = tx.QueryRow(ctx, `SELECT owner,revision FROM account_receivers
		WHERE account_id=$1 AND enabled AND owner<>'' AND lease_until>clock_timestamp()+interval '3 seconds'
		AND observed_at>clock_timestamp()-interval '15 seconds' AND observed_at<=clock_timestamp() FOR SHARE`,
		scope.Account).Scan(&out.Owner, &out.Revision)
	if err != nil || out.Owner != scope.ReceiverOwner || out.Revision != scope.ReceiverVersion {
		return out, Denied(409, "business_connection_changed")
	}
	var nonce, aad any
	if scope.Platform == "wsl" && out.Action == features.BusinessReply {
		value, e := reserveNativeNonce(ctx, tx)
		if e != nil {
			return out, e
		}
		out.Nonce, err = strconv.ParseUint(value, 10, 64)
		if err != nil {
			return out, err
		}
		var random [8]byte
		if _, err = rand.Read(random[:]); err != nil {
			return out, err
		}
		out.AAD = binary.LittleEndian.Uint64(random[:])
		if out.AAD == 0 {
			return out, Denied(503, "native_random_unavailable")
		}
		nonce, aad = value, strconv.FormatUint(out.AAD, 10)
	}
	err = tx.QueryRow(ctx, `INSERT INTO business_rule_attempts
		(job_id,platform,action,client_id,receiver_owner,receiver_revision,nonce,aad)
		VALUES($1,$2,$3,$4,$5,$6,$7::numeric,$8::numeric) RETURNING message_at`,
		id, scope.Platform, out.Action, out.ClientID, out.Owner, out.Revision, nonce, aad).Scan(&out.MessageAt)
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `UPDATE business_rule_jobs SET status='submitting',updated_at=clock_timestamp()
		WHERE id=$1 AND status='executing'`, id); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	out.RecordRoute = func(work context.Context, route nativewire.TextRoute) error {
		if scope.Platform != "wsl" || out.Action != features.BusinessReply ||
			len(route.Digest) != 32 || route.DeploymentVersion != scope.DeploymentVersion ||
			route.ObservedAt.After(time.Now()) || time.Since(route.ObservedAt) > 5*time.Second {
			return Denied(409, "native_text_route_unverified")
		}
		tag, e := s.DB.Platform.Exec(work, `UPDATE business_rule_attempts a SET route_digest=$2,route_deployment=$3
			FROM business_rule_jobs j WHERE j.id=a.job_id AND a.job_id=$1 AND j.status='submitting'
			AND a.route_digest IS NULL AND a.submitted_at IS NULL
			AND j.expires_at>clock_timestamp()+interval '3 seconds'`, id, route.Digest, route.DeploymentVersion)
		if e == nil && tag.RowsAffected() != 1 {
			e = Denied(409, "native_text_route_unverified")
		}
		return e
	}
	return out, nil
}

func (s *BusinessRulesService) lockAttempt(ctx context.Context, tx pgx.Tx, intent BusinessAttempt, submitted bool) error {
	var valid bool
	err := tx.QueryRow(ctx, `SELECT true FROM business_rule_jobs j
		JOIN business_rule_attempts a ON a.job_id=j.id JOIN account_receivers r ON r.account_id=j.account_id
		WHERE j.id=$1 AND j.status='submitting' AND j.scope_snapshot=$2
		AND j.expires_at>clock_timestamp()+make_interval(secs=>$6) AND a.client_id=$3 AND r.enabled
		AND r.owner=a.receiver_owner AND r.revision=a.receiver_revision
		AND r.lease_until>clock_timestamp()+make_interval(secs=>$6) AND r.observed_at<=clock_timestamp()
		AND r.observed_at>clock_timestamp()-interval '15 seconds'
		AND r.snapshot->>'state'='connected'
		AND ($4=(a.submitted_at IS NOT NULL))
		AND (a.platform<>'wsl' OR a.action<>'send_text' OR a.route_digest IS NOT NULL
			AND a.route_deployment=$5 AND EXISTS(SELECT 1 FROM native_deployment d WHERE d.version=a.route_deployment))
		FOR UPDATE OF j,a FOR KEY SHARE OF r`, intent.JobID, intent.Scope.snapshot(),
		intent.ClientID, submitted, intent.Scope.DeploymentVersion, businessWriteFence.Seconds()+1).Scan(&valid)
	if err != nil || !valid {
		return Denied(409, "business_attempt_changed")
	}
	return nil
}

func (s *BusinessRulesService) reserveWrite(ctx context.Context, intent BusinessAttempt,
	payload businessPayload, proof BusinessEvidence) error {
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = s.lockScope(ctx, tx, intent.Scope, payload, proof); err != nil {
		return err
	}
	if err = s.lockAttempt(ctx, tx, intent, false); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO business_rule_account_cooldowns(account_id,next_write_at)
		VALUES($1,'-infinity') ON CONFLICT DO NOTHING`, intent.Scope.Account); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE business_rule_account_cooldowns
		SET next_write_at=clock_timestamp()+make_interval(secs=>$2)
		WHERE account_id=$1 AND next_write_at<=clock_timestamp()`, intent.Scope.Account,
		(businessWriteFence + businessWriteGap).Seconds())
	if err != nil || tag.RowsAffected() != 1 {
		return Denied(409, "business_account_cooling")
	}
	var pending bool
	err = tx.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM native_setting_operations WHERE account_id=$1
			AND (status IN('preparing','submitting','unknown') OR updated_at>clock_timestamp()-interval '5 seconds'))
		OR EXISTS(SELECT 1 FROM operations WHERE account_id=$1 AND status IN('running','unknown'))`,
		intent.Scope.Account).Scan(&pending)
	if err != nil {
		return err
	}
	if pending {
		return Denied(409, "business_account_operation_pending")
	}
	if err = ReserveControlledNativeTx(ctx, tx, intent.Scope.Platform, intent.Scope.Group, "automatic", intent.JobID); err != nil {
		return err
	}
	switch intent.Action {
	case features.BusinessReply:
		tag, err = tx.Exec(ctx, `UPDATE business_rule_consumers c SET last_reply_at=clock_timestamp()
			FROM group_bindings b WHERE c.group_id=$1 AND b.group_id=c.group_id
			AND (c.last_reply_at IS NULL OR c.last_reply_at<=clock_timestamp()-make_interval(
				secs=>coalesce(nullif((b.config#>>'{feature_options,keyword_replies,cooldown_seconds}')::integer,0),30)))`,
			intent.Scope.Group)
	case features.BusinessMute:
		tag, err = tx.Exec(ctx, `INSERT INTO business_rule_member_cooldowns(group_id,account_id,member_id,until_at)
			VALUES($1,$2,$3,clock_timestamp()+interval '10 minutes')
			ON CONFLICT(group_id,account_id,member_id) DO UPDATE SET until_at=excluded.until_at
			WHERE business_rule_member_cooldowns.until_at<=clock_timestamp()`,
			intent.Scope.Group, intent.Scope.Account, intent.Sender)
	default:
		tag, err = tx.Exec(ctx, `SELECT 1`)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return Denied(409, "business_rule_cooling")
	}
	tag, err = tx.Exec(ctx, `UPDATE business_rule_attempts SET submitted_at=clock_timestamp()
		WHERE job_id=$1 AND submitted_at IS NULL`, intent.JobID)
	if err != nil || tag.RowsAffected() != 1 {
		return Denied(409, "business_write_already_attempted")
	}
	return tx.Commit(ctx)
}

func (s *BusinessRulesService) finish(ctx context.Context, id, state, code string) error {
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	tx, err := s.DB.Platform.Begin(work)
	if err != nil {
		return err
	}
	defer tx.Rollback(work)
	tag, err := tx.Exec(work, `UPDATE business_rule_jobs SET status=$2,result_code=$3,updated_at=clock_timestamp()
		WHERE id=$1 AND status IN('queued','executing','submitting')`, id, state, code)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	_, err = tx.Exec(work, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
		SELECT tenant_id,actor_id,'automatic_business_result',group_id::text,
			jsonb_build_object('job_id',id,'source','automatic','platform',platform,'rule',rule,
				'kind',action,'status',status,'code',result_code)
		FROM business_rule_jobs WHERE id=$1`, id)
	if err != nil {
		return err
	}
	return tx.Commit(work)
}

func businessErrorCode(err error) string {
	var failure *Failure
	if errors.As(err, &failure) {
		return failure.Code
	}
	return "business_unavailable"
}
