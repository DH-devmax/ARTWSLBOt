package control

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/assistant"
)

func (h *AssistantHost) Reserve(ctx context.Context, amount int64) (string, error) {
	if amount < 1 || amount > 200000 {
		return "", assistant.ErrLimit
	}
	if h.service.Gateway != nil {
		return "gateway", h.Check(ctx)
	}
	s := h.service
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798440)`); err != nil {
		return "", err
	}
	a, err := s.authorize(ctx, tx, h.ref, h.session.Scope, "assistant:use", true, true)
	if err != nil {
		return "", err
	}
	fence, err := controlledExecutionScope(ctx, tx, a.ID, h.session.Scope.Platform, h.session.Scope.GroupID)
	if err != nil {
		return "", err
	}
	if fence.ControlledScopeID != "" {
		return "", Denied(409, "controlled_onboarding_gateway_required")
	}
	var active bool
	err = tx.QueryRow(ctx, `SELECT status='running' AND NOT cancel_requested AND deadline>clock_timestamp()
        FROM assistant_turns WHERE id=$1 FOR UPDATE`, h.turnID).Scan(&active)
	if err != nil {
		return "", err
	}
	if !active {
		return "", context.Canceled
	}
	var globalLimit, tenantLimit, globalUsed, tenantUsed int64
	err = tx.QueryRow(ctx, `SELECT monthly_tokens FROM assistant_settings WHERE singleton FOR SHARE`).Scan(&globalLimit)
	if err != nil {
		return "", err
	}
	tenantLimit = globalLimit
	if a.Role != "superadmin" {
		err = tx.QueryRow(ctx, `SELECT p.monthly_assistant_tokens FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
            WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>clock_timestamp()
            AND 'management_assistant'=ANY(p.modules) FOR SHARE OF sub,p`, a.TenantID).Scan(&tenantLimit)
		if err == pgx.ErrNoRows {
			return "", Denied(403, "active_subscription_required")
		}
		if err != nil {
			return "", err
		}
	}
	err = tx.QueryRow(ctx, `SELECT coalesce(sum(CASE WHEN state='actual' THEN charged ELSE greatest(reserved,charged) END),0)::bigint,
        coalesce(sum(CASE WHEN state='actual' THEN charged ELSE greatest(reserved,charged) END)
            FILTER(WHERE coalesce(tenant_id::text,'')=$1),0)::bigint
        FROM assistant_usage WHERE month=date_trunc('month',clock_timestamp() AT TIME ZONE 'Asia/Shanghai')::date`,
		a.TenantID).Scan(&globalUsed, &tenantUsed)
	if err != nil {
		return "", err
	}
	if amount > globalLimit-globalUsed || amount > tenantLimit-tenantUsed {
		return "", Denied(409, "assistant_budget_exhausted")
	}
	id := NewID()
	_, err = tx.Exec(ctx, `INSERT INTO assistant_usage(id,turn_id,actor_id,tenant_id,month,reserved,state,domain)
        VALUES($1,$2,$3,nullif($4,'')::uuid,date_trunc('month',clock_timestamp() AT TIME ZONE 'Asia/Shanghai')::date,$5,'reserved',$6)`,
		id, h.turnID, a.ID, a.TenantID, amount, h.session.Scope.Platform)
	if err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func (h *AssistantHost) Settle(ctx context.Context, id string, usage assistant.Usage, unknown bool) error {
	if h.service.Gateway != nil {
		if id != "gateway" {
			return assistant.ErrInvalid
		}
		return nil
	}
	state := "unknown"
	var charged int64
	if usage.Valid() {
		state, charged = "actual", usage.TotalTokens
	} else if !unknown {
		state = "estimated"
	}
	// Unknown/missing usage keeps the full reservation charged; never release as zero.
	_, err := h.service.DB.Platform.Exec(ctx, `UPDATE assistant_usage SET state=$3,
        charged=CASE WHEN $3='actual' THEN $4 ELSE greatest(reserved,$4) END
        WHERE id=$1 AND turn_id=$2 AND state='reserved'`, id, h.turnID, state, charged)
	return err
}

func (s *AssistantService) Usage(ctx context.Context, ref SessionRef) (map[string]any, error) {
	a, err := s.authorize(ctx, s.DB.Platform, ref, assistant.Scope{Kind: "overview"}, "assistant:use", false, false)
	if err != nil {
		return nil, err
	}
	if s.Gateway != nil {
		out, err := s.Gateway.Usage(ctx, ref)
		if err != nil {
			return nil, err
		}
		if a.Role != "superadmin" {
			// Keep the existing assistant API's source label. New consumers can
			// use charge_source, or GatewayService.Usage's canonical source.
			out["charge_source"], out["billing_source"] = out["billing_source"], "shared_gateway"
		}
		return out, nil
	}
	var used, held int64
	var month time.Time
	if a.Role == "superadmin" {
		err = s.DB.Platform.QueryRow(ctx, `SELECT date_trunc('month',clock_timestamp() AT TIME ZONE 'Asia/Shanghai')::date,
        coalesce(sum(CASE WHEN state='actual' THEN charged ELSE greatest(reserved,charged) END),0)::bigint,
        coalesce(sum(reserved) FILTER(WHERE state IN ('reserved','unknown','estimated')),0)::bigint
        FROM assistant_usage WHERE month=date_trunc('month',clock_timestamp() AT TIME ZONE 'Asia/Shanghai')::date
        AND actor_id=$1`, a.ID).Scan(&month, &used, &held)
	} else {
		err = s.read(ctx, a, ref.assistantPlatform(), func(q Queryer) error {
			return q.QueryRow(ctx, `SELECT date_trunc('month',now() AT TIME ZONE 'Asia/Shanghai')::date,used,held
                FROM assistant_month_usage()`).Scan(&month, &used, &held)
		})
	}
	if err != nil {
		return nil, err
	}
	var limit int64
	if a.Role == "superadmin" {
		err = s.DB.Platform.QueryRow(ctx, `SELECT monthly_tokens FROM assistant_settings WHERE singleton`).Scan(&limit)
	} else {
		err = s.DB.Platform.QueryRow(ctx, `SELECT p.monthly_assistant_tokens FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
            WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>clock_timestamp()`, a.TenantID).Scan(&limit)
	}
	if err != nil {
		return nil, err
	}
	_, err = s.authorize(ctx, s.DB.Platform, ref, assistant.Scope{Kind: "overview"}, "assistant:use", false, false)
	return map[string]any{"month": month.Format("2006-01"), "timezone": "Asia/Shanghai", "used_or_reserved": used,
		"unverified_reservations": held, "limit": limit, "unlimited": false,
		"billing_source": "legacy"}, err
}
