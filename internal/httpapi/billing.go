package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"wangshangliao/internal/control"
)

func (s *Server) billingRoutes() {
	s.handle("GET /api/v1/billing", false, s.billing)
	s.handle("GET /api/v1/billing/plans", false, s.commercialPlans)
	s.handle("POST /api/v1/billing/confirm", true, s.confirmPayment)
	s.handle("PUT /api/v1/billing/limits", false, s.creditLimit)
	s.handle("POST /api/v1/billing/plans", true, s.publishCommercialPlan)
	s.handle("PUT /api/v1/billing/plans/{id}/availability", true, s.commercialPlanAvailability)
}
func (s *Server) commercialPlans(w http.ResponseWriter, r *http.Request, p Principal) error {
	var data []byte
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
        SELECT c.id,c.code,c.version,c.enabled,c.price_micro_usdt,c.credits_micro,p.group_limit,p.ai_group_limit,p.member_limit,p.allowed_platforms
        FROM commercial_plans c JOIN plans p ON p.id=c.plan_id WHERE c.enabled OR $1 ORDER BY c.code,c.version DESC) x`, p.Role == "superadmin").Scan(&data)
	if err != nil {
		return err
	}
	if p.Role == "superadmin" {
		var plans []map[string]any
		if err = json.Unmarshal(data, &plans); err != nil {
			return err
		}
		plans = append(plans, map[string]any{"id": "operator_unlimited", "code": "SUPERADMIN",
			"name": "超管专属 · 不限额", "version": 1, "system_only": true, "saleable": false, "assignable": false,
			"enabled": false, "permanent": true, "unlimited": true, "price_micro_usdt": nil, "credits_micro": nil,
			"group_limit": nil, "ai_group_limit": nil, "member_limit": nil, "allowed_platforms": []string{"wsl", "lajiaoliao"}})
		send(w, 200, plans)
	} else {
		send(w, 200, json.RawMessage(data))
	}
	return nil
}
func (s *Server) publishCommercialPlan(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Code string `json:"code"`
		Base int64  `json:"base_version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	id, err := (&control.BillingService{DB: s.DB}).PublishPlan(r.Context(), principalActor(p), in.Code, in.Base)
	if err != nil {
		return err
	}
	send(w, 201, map[string]string{"id": id})
	return nil
}
func (s *Server) commercialPlanAvailability(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Enabled  bool  `json:"enabled"`
		Expected *bool `json:"expected_enabled"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !validUUID(r.PathValue("id")) || in.Expected == nil {
		return fail(400, "invalid_plan")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `UPDATE commercial_plans SET enabled=$2 WHERE id=$1 AND enabled=$3`, r.PathValue("id"), in.Enabled, *in.Expected)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fail(409, "version_conflict")
	}
	if err = auditTx(r.Context(), tx, p, "", "commercial_plan_availability", r.PathValue("id"), map[string]bool{"enabled": in.Enabled}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]bool{"saved": true})
	return nil
}
func (s *Server) confirmPayment(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in control.PaymentConfirmation
	if err := decode(w, r, &in); err != nil {
		return err
	}
	out, err := (&control.BillingService{DB: s.DB}).Confirm(r.Context(), principalActor(p), in)
	if err != nil {
		return err
	}
	send(w, 200, out)
	return nil
}
func (s *Server) billing(w http.ResponseWriter, r *http.Request, p Principal) error {
	tenant := p.TenantID
	if p.Role == "superadmin" {
		tenant = r.URL.Query().Get("tenant_id")
		space, err := control.OperatorWorkspace(r.Context(), s.DB.Platform, p.ID)
		if err != nil {
			return err
		}
		if tenant == "" || tenant == space.ID {
			fields, err := s.centerWorkspaceFields(r, p)
			if err != nil {
				return err
			}
			usage, err := s.Gateway.UsageForCenter(r.Context(), assistantRef(r))
			if err != nil {
				return err
			}
			send(w, 200, mergeCenterFields(map[string]any{"full_bill": true, "periods": []any{},
				"orders": []any{}, "entries": []any{}, "wallet": nil, "usage": usage}, fields))
			return nil
		}
	}
	if !validUUID(tenant) {
		return fail(400, "tenant_required")
	}
	if err := (&control.BillingService{DB: s.DB}).Refresh(r.Context(), tenant); err != nil {
		return err
	}
	var owner bool
	if err := s.DB.Platform.QueryRow(r.Context(), `SELECT coalesce(owner_id=$2,false) FROM tenants WHERE id=$1`, tenant, p.ID).Scan(&owner); err != nil {
		return err
	}
	full := p.Role == "superadmin" || owner
	if owner {
		grants, err := control.EffectivePermissions(r.Context(), s.DB.Platform, principalActor(p), tenantPermissions)
		if err != nil {
			return err
		}
		full = slices.Contains(grants, "billing:read")
	}
	var data []byte
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT jsonb_build_object(
        'full_bill',$3::boolean,
        'periods',CASE WHEN $3 THEN (SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
            SELECT s.id,s.starts_at,s.ends_at,s.state,s.reason,c.code,c.price_micro_usdt,c.credits_micro
            FROM subscription_periods s JOIN commercial_plans c ON c.id=s.plan_version_id WHERE s.tenant_id=$1
            ORDER BY starts_at DESC LIMIT 24) x) ELSE '[]'::jsonb END,
        'wallet',CASE WHEN $3 THEN (SELECT to_jsonb(x) FROM (
            SELECT w.*,w.granted_micro-w.spent_micro-w.held_micro available_micro FROM ai_wallets w
            JOIN subscription_periods s ON s.id=w.period_id WHERE w.tenant_id=$1 AND s.state='active'
            AND s.starts_at<=clock_timestamp() AND s.ends_at>clock_timestamp()) x) ELSE NULL END,
        'calls',(SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
            SELECT id,actor_id,group_id,purpose,reserved_micro,charged_micro,effective_charged_micro,status,created_at,finished_at
            FROM ai_call_costs WHERE tenant_id=$1 AND ($3 OR actor_id=$2)
            AND (group_id IS NULL OR $3 OR member_group_visible(tenant_id,group_id,$2)) ORDER BY created_at DESC LIMIT 100) x),
        'limits',CASE WHEN $3 THEN (SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
            SELECT kind,scope,limit_micro,version FROM ai_credit_limits WHERE tenant_id=$1) x) ELSE '[]'::jsonb END,
        'entries',CASE WHEN $3 THEN (SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
            SELECT kind,amount_micro,reason,created_at FROM credit_entries WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 100) x) ELSE '[]'::jsonb END)`,
		tenant, p.ID, full).Scan(&data)
	if err != nil {
		return err
	}
	send(w, 200, json.RawMessage(data))
	return nil
}
func (s *Server) creditLimit(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Kind    string `json:"kind"`
		Scope   string `json:"scope"`
		Limit   int64  `json:"limit_micro"`
		Version int64  `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Limit < 0 || in.Version < 0 || in.Kind != "purpose" && in.Kind != "member" && in.Kind != "group" ||
		in.Kind == "purpose" && (in.Scope != "assistant" && in.Scope != "heatbot") ||
		in.Kind != "purpose" && !validUUID(in.Scope) {
		return fail(400, "invalid_credit_limit")
	}
	tx, err := s.memberTx(r, p)
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(798460)`); err != nil {
		return err
	}
	var owner bool
	if err = tx.QueryRow(r.Context(), `SELECT coalesce(owner_id=$2,false) FROM tenants WHERE id=$1`, p.TenantID, p.ID).Scan(&owner); err != nil {
		return err
	}
	if !owner {
		return fail(403, "tenant_owner_required")
	}
	if in.Kind == "group" {
		if err = control.RequireGroup(r.Context(), tx, principalActor(p), in.Scope); err != nil {
			return err
		}
	}
	if in.Kind == "member" {
		var ok bool
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND tenant_id=$2)`, in.Scope, p.TenantID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return fail(404, "member_not_found")
		}
	}
	tag, err := tx.Exec(r.Context(), `INSERT INTO ai_credit_limits(tenant_id,kind,scope,limit_micro) SELECT $1,$2,$3,$4 WHERE $5::bigint=0
        ON CONFLICT(tenant_id,kind,scope) DO UPDATE SET limit_micro=excluded.limit_micro,version=ai_credit_limits.version+1
        WHERE ai_credit_limits.version=$5`, p.TenantID, in.Kind, in.Scope, in.Limit, in.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fail(409, "version_conflict")
	}
	if err = auditTx(r.Context(), tx, p, p.TenantID, "ai_credit_limit_changed", in.Scope, map[string]any{"kind": in.Kind, "limit_micro": in.Limit}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]bool{"saved": true})
	return nil
}

func (s *Server) RunBillingMaintenance(ctx context.Context) error {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		work, stop := context.WithTimeout(ctx, 5*time.Second)
		_ = s.Gateway.PruneGrants(work)
		if s.HeatChat != nil {
			_ = s.HeatChat.Prune(work)
		}
		rows, err := s.DB.Platform.Query(work, `SELECT DISTINCT tenant_id::text FROM subscription_periods
            WHERE state IN ('scheduled','blocked','active') AND starts_at<=clock_timestamp()`)
		var tenants []string
		if err == nil {
			for rows.Next() {
				var id string
				if err = rows.Scan(&id); err != nil {
					break
				}
				tenants = append(tenants, id)
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
		}
		stop()
		if err == nil {
			for _, tenant := range tenants {
				work, stop = context.WithTimeout(ctx, 3*time.Second)
				_ = (&control.BillingService{DB: s.DB}).Refresh(work, tenant)
				stop()
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
