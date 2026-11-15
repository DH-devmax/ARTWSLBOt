package control

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/nativewire"
)

type AssistantMessageAttempt struct {
	ID, Platform, ClientID, Owner, Sender string
	Revision                              int64
	Nonce, AAD                            uint64
	CreatedAt                             time.Time
}

func (b *assistantBusiness) prepareMessage(ctx context.Context, tx pgx.Tx, id string) error {
	p := b.plan
	if p.Action != "send_text" && p.Action != "recall_message" {
		return nil
	}
	var owner string
	var revision int64
	err := tx.QueryRow(ctx, `SELECT owner,revision FROM account_receivers WHERE account_id=$1 AND enabled
		AND owner<>'' AND lease_until>clock_timestamp()+interval '3 seconds'
		AND observed_at>clock_timestamp()-interval '15 seconds' FOR SHARE`, p.Scope.AccountID).Scan(&owner, &revision)
	if err != nil {
		return Denied(409, "native_text_connection_changed")
	}
	var nonce, aad any
	if p.Scope.Platform == "wsl" {
		value, err := reserveNativeNonce(ctx, tx)
		if err != nil {
			return err
		}
		var random [8]byte
		if _, err = rand.Read(random[:]); err != nil || binary.LittleEndian.Uint64(random[:]) == 0 {
			return Denied(503, "native_random_unavailable")
		}
		nonce, aad = value, strconv.FormatUint(binary.LittleEndian.Uint64(random[:]), 10)
	}
	_, attempts := b.tables()
	_, err = tx.Exec(ctx, `INSERT INTO `+attempts+`
		(operation_id,platform,client_id,receiver_owner,receiver_revision,nonce,aad)
		VALUES($1,$2,$3,$4,$5,$6::numeric,$7::numeric)`, id, p.Scope.Platform, NewID(), owner, revision, nonce, aad)
	return err
}

func (s *NativeSettingsService) AssistantAttempt(ctx context.Context, ref SessionRef, service *AssistantService, id string) (AssistantMessageAttempt, error) {
	p, err := service.ActionPlan(ctx, ref, id)
	if err != nil {
		return AssistantMessageAttempt{}, err
	}
	return s.humanMessageAttempt(ctx, p, "assistant_action_plans", "assistant_message_attempts")
}

func (s *NativeSettingsService) ManualAttempt(ctx context.Context, ref SessionRef, service *ManualActionService, group, id string) (AssistantMessageAttempt, error) {
	p, err := service.Plan(ctx, ref, group, id)
	if err != nil {
		return AssistantMessageAttempt{}, err
	}
	return s.humanMessageAttempt(ctx, p, "manual_action_plans", "manual_message_attempts")
}

func (s *NativeSettingsService) humanMessageAttempt(ctx context.Context, p AssistantActionPlan, plans, attempts string) (AssistantMessageAttempt, error) {
	var out AssistantMessageAttempt
	var nonce, aad string
	err := s.DB.Platform.QueryRow(ctx, `SELECT o.id::text,t.platform,t.client_id::text,t.receiver_owner,t.receiver_revision,
		coalesce(t.nonce::text,'0'),coalesce(t.aad::text,'0'),t.message_at,a.native_uid
		FROM `+attempts+` t JOIN native_setting_operations o ON o.id=t.operation_id
		JOIN `+plans+` ap ON ap.id::text=o.idempotency_key AND ap.actor_id=o.actor_id
		JOIN accounts a ON a.id=o.account_id WHERE ap.id=$1 AND ap.domain=$2 AND o.account_id=$3
		AND o.group_id=$4 AND o.kind=$5 AND o.status='preparing'
		AND ap.status IN('approved','submitting') AND ap.expires_at>clock_timestamp()+interval '3 seconds'`,
		p.ID, p.Scope.Platform, p.Scope.AccountID, p.Scope.GroupID, p.Action).
		Scan(&out.ID, &out.Platform, &out.ClientID, &out.Owner, &out.Revision, &nonce, &aad, &out.CreatedAt, &out.Sender)
	if err != nil {
		return out, err
	}
	out.Nonce, err = strconv.ParseUint(nonce, 10, 64)
	if err == nil {
		out.AAD, err = strconv.ParseUint(aad, 10, 64)
	}
	return out, err
}

func (s *NativeSettingsService) RecordAssistantRoute(ctx context.Context, attempt AssistantMessageAttempt, route nativewire.TextRoute) error {
	return s.recordHumanRoute(ctx, attempt, route, "assistant_message_attempts")
}

func (s *NativeSettingsService) RecordManualRoute(ctx context.Context, attempt AssistantMessageAttempt, route nativewire.TextRoute) error {
	return s.recordHumanRoute(ctx, attempt, route, "manual_message_attempts")
}

func (s *NativeSettingsService) recordHumanRoute(ctx context.Context, attempt AssistantMessageAttempt, route nativewire.TextRoute, table string) error {
	if attempt.Platform != "wsl" || len(route.Digest) != 32 || route.DeploymentVersion < 1 ||
		route.ObservedAt.After(time.Now()) || time.Since(route.ObservedAt) > 5*time.Second {
		return Denied(409, "native_text_route_unverified")
	}
	tag, err := s.DB.Platform.Exec(ctx, `UPDATE `+table+` t SET route_digest=$2,route_deployment=$3
		FROM native_setting_operations o WHERE o.id=t.operation_id AND t.operation_id=$1
		AND t.platform='wsl' AND o.status='preparing' AND t.route_digest IS NULL
		AND o.expires_at>clock_timestamp()+interval '3 seconds'`, attempt.ID, route.Digest, route.DeploymentVersion)
	if err == nil && tag.RowsAffected() != 1 {
		return Denied(409, "native_text_route_unverified")
	}
	return err
}

func (b *assistantBusiness) messageFence(ctx context.Context, tx pgx.Tx, req SettingRequest, proof *SettingEvidence) (string, int64, error) {
	p := b.plan
	if proof != nil {
		if proof.Role != "member" && proof.Role != "admin" && proof.Role != "owner" || !proof.ModerationAllowed {
			return "", 0, Denied(403, "native_moderation_role_required")
		}
		if p.Action == "recall_message" {
			if p.message == nil || !p.message.Fresh(time.Now()) || proof.Role == "member" {
				return "", 0, Denied(409, "native_message_reference_expired")
			}
			if p.message.Sender != proof.UID &&
				(proof.TargetRole == "" || proof.TargetRole == "owner" || proof.Role == "admin" && proof.TargetRole != "member") {
				return "", 0, Denied(403, "native_moderation_target_protected")
			}
		}
		var allowed bool
		_, attempts := b.tables()
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+attempts+` t
			JOIN native_setting_operations o ON o.id=t.operation_id JOIN account_receivers r ON r.account_id=o.account_id
			WHERE o.idempotency_key=$1 AND o.account_id=$2 AND o.status='submitting'
			AND r.enabled AND r.owner=t.receiver_owner AND r.revision=t.receiver_revision
			AND r.lease_until>clock_timestamp()+interval '3 seconds'
			AND (o.kind<>'send_text' OR t.platform='lajiaoliao' OR t.route_digest IS NOT NULL))`,
			p.ID, p.Scope.AccountID).Scan(&allowed)
		if err != nil {
			return "", 0, err
		}
		if !allowed {
			return "", 0, Denied(409, "native_text_connection_changed")
		}
	}
	return p.Scope.GroupID, p.BindingEpoch, nil
}
