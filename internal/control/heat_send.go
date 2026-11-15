package control

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"strconv"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/nativewire"
)

// HeatSend is constructed by control from a durable job, never from a model or
// HTTP request. The port must bind it to the existing connection and one write.
type HeatSend struct {
	JobID, ClientID, Owner, Text string
	Scope                        HeatScope
	Revision                     int64
	Nonce, AAD                   uint64
	MessageAt                    time.Time
	RecordRoute                  func(context.Context, nativewire.TextRoute) error
}

func (s *HeatChatService) lockSendScope(ctx context.Context, tx pgx.Tx, expected HeatScope) error {
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='750ms'`); err != nil {
		return err
	}
	// These rows are also used by account, binding, subscription and settings
	// changes. The locks last only for the bounded final socket write.
	var id string
	err := tx.QueryRow(ctx, `SELECT a.id::text FROM accounts a
		JOIN group_bindings b ON b.ai_account_id=a.id JOIN native_groups g ON g.id=b.group_id
		JOIN tenants t ON t.id=b.tenant_id JOIN users u ON u.id=t.owner_id
		JOIN group_business_runtime br ON br.group_id=b.group_id AND br.tenant_id=b.tenant_id
		JOIN heat_chat_admission h ON h.platform=g.platform
		JOIN ai_model_routes r ON r.alias='default'
		WHERE b.group_id=$1 AND a.id=$2
		FOR SHARE OF a,b,g,t,u,br,h,r`, expected.Group, expected.Account).Scan(&id)
	if err != nil {
		return Denied(409, "heat_chat_scope_changed")
	}
	if !expected.Service.Demo {
		err = tx.QueryRow(ctx, `SELECT sub.tenant_id::text FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
			WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>clock_timestamp()+interval '3 seconds'
			FOR SHARE OF sub,p`, expected.Tenant).Scan(&id)
		if err != nil {
			return Denied(409, "heat_chat_scope_changed")
		}
	}
	if expected.Platform == "wsl" {
		var version int64
		if err = tx.QueryRow(ctx, `SELECT version FROM native_deployment FOR SHARE`).Scan(&version); err != nil || version < 1 {
			return Denied(409, "heat_chat_native_deployment_unavailable")
		}
	}
	// Lock optional settings separately: PostgreSQL cannot lock the nullable
	// side of an outer join.
	if _, err = tx.Exec(ctx, `SELECT version FROM maibot_settings
		WHERE account_id=$1 AND section='chat' FOR SHARE`, expected.Account); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT version FROM group_chat_preferences WHERE group_id=$1 FOR SHARE`, expected.Group); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT version FROM personas WHERE account_id=$1 FOR SHARE`, expected.PersonaAccount); err != nil {
		return err
	}
	current, err := s.heatScope(ctx, tx, expected.Group, true)
	if err != nil || !bytes.Equal(current.digest(), expected.digest()) || !current.Effective.allows(time.Now()) {
		return Denied(409, "heat_chat_scope_changed")
	}
	if s.Gateway == nil {
		return Denied(503, "heat_chat_gateway_unavailable")
	}
	if err = current.Service.requireGroup(ctx, tx, expected.Group); err != nil {
		return err
	}
	if err = current.Service.requirePermission(ctx, tx, s.Gateway.Catalog, "groups:ai"); err != nil {
		return err
	}
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM account_groups WHERE group_id=$1 AND account_id=$2
		AND observation_source='native' AND observed_at<=clock_timestamp()
		AND observed_at>clock_timestamp()-interval '60 seconds' AND role IN('member','admin','owner') FOR SHARE`,
		expected.Group, expected.Account).Scan(&role)
	if err != nil {
		return Denied(409, "heat_chat_native_role_unavailable")
	}
	return nil
}

func (s *HeatChatService) send(ctx context.Context, id string, scope HeatScope) error {
	if s.FreshRole == nil || s.Send == nil {
		return Denied(503, "heat_chat_runtime_unavailable")
	}
	if err := s.FreshRole(ctx, scope); err != nil {
		return s.finish(ctx, id, "cancelled", "heat_chat_native_role_unavailable")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = s.lockSendScope(ctx, tx, scope); err != nil {
		_ = tx.Rollback(ctx)
		_ = s.finish(ctx, id, "cancelled", heatErrorCode(err))
		return err
	}
	var sealed []byte
	var digest []byte
	var expiry time.Time
	err = tx.QueryRow(ctx, `SELECT response_sealed,scope_digest,expires_at FROM heat_chat_jobs
		WHERE id=$1 AND status='generated' AND expires_at>clock_timestamp()+interval '3 seconds' FOR UPDATE`, id).
		Scan(&sealed, &digest, &expiry)
	if err != nil {
		return err
	}
	if !bytes.Equal(scope.digest(), digest) {
		return Denied(409, "heat_chat_scope_changed")
	}
	var paid bool
	// Missing or unknown usage stays frozen and cannot authorize a native send.
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_calls WHERE external_key=$1
		AND tenant_id=$2 AND group_id=$3 AND actor_id=$4 AND purpose='heatbot' AND status='settled'
		AND (NOT $5 OR execution_mode='workspace_demo' AND billing_source='platform_workspace'
			AND service_task_id=$6 AND period_id IS NULL))`,
		"heat-job:"+id, scope.Tenant, scope.Group, scope.Actor, scope.Service.Demo, id).Scan(&paid); err != nil {
		return err
	}
	if !paid {
		_ = tx.Rollback(ctx)
		_ = s.finish(ctx, id, "unknown", "heat_chat_usage_unconfirmed")
		return nil
	}
	raw, err := s.Vault.Open("heat-chat-response:"+id, sealed)
	if err != nil {
		return err
	}
	var text string
	err = json.Unmarshal(raw, &text)
	clear(raw)
	if err != nil || len(text) == 0 || len(text) > 4096 || !utf8.ValidString(text) {
		return Denied(409, "heat_chat_response_invalid")
	}
	var exists, cooling bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM heat_chat_send_attempts WHERE job_id=$1),
		EXISTS(SELECT 1 FROM heat_chat_consumers WHERE group_id=$2
			AND last_send_at>clock_timestamp()-$3::double precision*interval '1 second')`,
		id, scope.Group, scope.Effective.CooldownSeconds).Scan(&exists, &cooling); err != nil {
		return err
	}
	if exists {
		return Denied(409, "heat_chat_send_already_recorded")
	}
	if cooling {
		return nil
	}
	if _, err = tx.Exec(ctx, `INSERT INTO heat_chat_account_cooldowns(account_id,next_send_at)
		VALUES($1,'-infinity') ON CONFLICT DO NOTHING`, scope.Account); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT next_send_at>clock_timestamp() FROM heat_chat_account_cooldowns
		WHERE account_id=$1 FOR UPDATE`, scope.Account).Scan(&cooling); err != nil {
		return err
	}
	if cooling {
		return nil
	}
	intent := HeatSend{JobID: id, Scope: scope, ClientID: NewID(), Text: text}
	err = tx.QueryRow(ctx, `SELECT owner,revision FROM account_receivers WHERE account_id=$1 AND enabled
		AND owner<>'' AND lease_until>clock_timestamp()+interval '3 seconds'
		AND observed_at>clock_timestamp()-interval '15 seconds' FOR SHARE`, scope.Account).Scan(&intent.Owner, &intent.Revision)
	if err != nil {
		return Denied(409, "heat_chat_connection_unavailable")
	}
	var nonce, aad any
	if scope.Platform == "wsl" {
		var value string
		value, err = reserveNativeNonce(ctx, tx)
		if err == nil {
			intent.Nonce, err = strconv.ParseUint(value, 10, 64)
		}
		if err != nil {
			return err
		}
		var random [8]byte
		if _, err = rand.Read(random[:]); err != nil || binary.LittleEndian.Uint64(random[:]) == 0 {
			return Denied(503, "native_random_unavailable")
		}
		intent.AAD = binary.LittleEndian.Uint64(random[:])
		nonce, aad = value, strconv.FormatUint(intent.AAD, 10)
	}
	err = tx.QueryRow(ctx, `INSERT INTO heat_chat_send_attempts(job_id,platform,client_id,
		receiver_owner,receiver_revision,nonce,aad) VALUES($1,$2,$3,$4,$5,$6::numeric,$7::numeric)
		RETURNING message_at`, id, scope.Platform, intent.ClientID, intent.Owner, intent.Revision, nonce, aad).Scan(&intent.MessageAt)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE heat_chat_jobs SET status='sending',updated_at=clock_timestamp() WHERE id=$1`, id); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err // A result-unknown commit is never followed by a socket write.
	}
	intent.RecordRoute = func(work context.Context, route nativewire.TextRoute) error {
		if scope.Platform != "wsl" || len(route.Digest) != 32 || route.DeploymentVersion < 1 ||
			route.ObservedAt.After(time.Now()) || time.Since(route.ObservedAt) > 5*time.Second {
			return Denied(409, "native_text_route_unverified")
		}
		tag, e := s.DB.Platform.Exec(work, `UPDATE heat_chat_send_attempts a SET route_digest=$2,route_deployment=$3
			FROM heat_chat_jobs j WHERE j.id=a.job_id AND a.job_id=$1 AND j.status='sending'
			AND a.route_digest IS NULL AND j.expires_at>clock_timestamp()+interval '3 seconds'`, id, route.Digest, route.DeploymentVersion)
		if e == nil && tag.RowsAffected() != 1 {
			e = Denied(409, "native_text_route_unverified")
		}
		return e
	}
	var called, attempted atomic.Bool
	fence := func(work context.Context, write func(context.Context) error) error {
		if !called.CompareAndSwap(false, true) {
			return Denied(409, "heat_chat_write_already_attempted")
		}
		final, cancel := context.WithTimeout(work, 2*time.Second)
		defer cancel()
		// Reserve durably immediately before the two-second write fence. Seven
		// seconds between reservations guarantees at least five between writes,
		// including a timeout or an unknown commit outcome.
		reserve, e := s.DB.Platform.Begin(final)
		if e != nil {
			return e
		}
		defer reserve.Rollback(final)
		if e = scope.controlledExecutionFence.require(final, reserve, "", scope.Platform, scope.Group); e != nil {
			return e
		}
		if e = ReserveControlledNativeTx(final, reserve, scope.Platform, scope.Group, "heatbot", id); e != nil {
			return e
		}
		reserved, e := reserve.Exec(final, `UPDATE heat_chat_account_cooldowns
			SET next_send_at=clock_timestamp()+interval '7 seconds'
			WHERE account_id=$1 AND next_send_at<=clock_timestamp()`, scope.Account)
		if e != nil {
			return e
		}
		if reserved.RowsAffected() != 1 {
			return Denied(409, "heat_chat_account_cooling")
		}
		if _, e = reserve.Exec(final, `UPDATE heat_chat_consumers SET last_send_at=clock_timestamp()
			WHERE group_id=$1`, scope.Group); e != nil {
			return e
		}
		if e = reserve.Commit(final); e != nil {
			return e
		}
		commit, e := s.DB.Platform.Begin(final)
		if e != nil {
			return e
		}
		defer commit.Rollback(final)
		if e = s.lockSendScope(final, commit, scope); e != nil {
			return e
		}
		var valid bool
		e = commit.QueryRow(final, `SELECT EXISTS(SELECT 1 FROM heat_chat_send_attempts a
			JOIN heat_chat_jobs j ON j.id=a.job_id JOIN account_receivers r ON r.account_id=j.account_id
			WHERE j.id=$1 AND j.status='sending' AND j.scope_digest=$2
			AND j.expires_at>clock_timestamp()+interval '2 seconds' AND r.enabled
			AND r.owner=a.receiver_owner AND r.revision=a.receiver_revision
			AND r.lease_until>clock_timestamp()+interval '2 seconds'
			AND r.observed_at>clock_timestamp()-interval '15 seconds'
			AND (a.platform='lajiaoliao' OR a.route_digest IS NOT NULL AND EXISTS(
				SELECT 1 FROM native_deployment d WHERE d.version=a.route_deployment)))`, id, scope.digest()).Scan(&valid)
		if e != nil {
			return e
		}
		if !valid {
			return Denied(409, "heat_chat_connection_changed")
		}
		if e = scope.controlledExecutionFence.requireSubmission(final, commit, 2*time.Second); e != nil {
			return e
		}
		attempted.Store(true)
		if e = write(final); e != nil {
			return e
		}
		return commit.Commit(final)
	}
	work, cancel := context.WithDeadline(ctx, minHeatDeadline(expiry, time.Now().Add(20*time.Second)))
	confirmed, sendErr := s.Send(work, intent, fence)
	cancel()
	state, code := "failed", "heat_chat_send_not_submitted"
	if attempted.Load() {
		state, code = "unknown", "heat_chat_send_unconfirmed"
		if sendErr == nil && confirmed {
			state, code = "sent", "heat_chat_sent"
		}
	}
	save, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer stop()
	result, e := s.DB.Platform.Begin(save)
	if e != nil {
		return e
	}
	defer result.Rollback(save)
	tag, e := result.Exec(save, `UPDATE heat_chat_jobs SET status=$2,result_code=$3,updated_at=clock_timestamp()
		WHERE id=$1 AND status='sending'`, id, state, code)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 1 && attempted.Load() {
		if _, e = result.Exec(save, `UPDATE heat_chat_consumers SET last_send_at=clock_timestamp() WHERE group_id=$1`, scope.Group); e != nil {
			return e
		}
	}
	if e = result.Commit(save); e != nil {
		return e
	}
	if state == "sent" {
		s.sends.Add(1)
	} else {
		s.blocked.Add(1)
	}
	return nil
}
