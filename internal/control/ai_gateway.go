package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/aigateway"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/database"
	"wangshangliao/internal/security"
)

type GatewayService struct {
	DB               *database.DB
	Vault            *security.Vault
	Catalog          []PermissionSpec
	Scheduler        *aigateway.Scheduler
	ProviderFactory  func(assistant.ModelConfig) (assistant.Provider, error)
	InspectionClient *http.Client
}
type GatewaySettings struct {
	Enabled       bool  `json:"enabled"`
	PlatformLimit int64 `json:"platform_limit_micro"`
	TestLimit     int64 `json:"test_limit_micro"`
	Version       int64 `json:"version"`
}
type ModelRoute struct {
	Alias         string                `json:"alias"`
	Version       int64                 `json:"version"`
	Enabled       bool                  `json:"enabled"`
	Verified      bool                  `json:"verified"`
	USDUSDTParity bool                  `json:"usd_usdt_parity"`
	Rates         aigateway.Rates       `json:"rates"`
	Model         assistant.ModelConfig `json:"model"`
}

// GatewayProviderConfig keeps the human-confirmed accounting policy in the same
// immutable, sealed snapshot as its provider. Transport reads only ModelConfig.
type GatewayProviderConfig struct {
	assistant.ModelConfig
	USDUSDTParity bool `json:"usd_usdt_parity"`
}

func (s *GatewayService) Configure(ctx context.Context, a Actor, in GatewaySettings, route ModelRoute) error {
	if a.Role != "superadmin" {
		return Denied(403, "superadmin_required")
	}
	var modelErr error
	route.Model, modelErr = normalizedGatewayModel(route.Model)
	if modelErr != nil {
		return modelErr
	}
	if in.PlatformLimit < 0 || in.TestLimit < 0 || in.TestLimit > in.PlatformLimit || route.Alias != "default" ||
		route.Model.Validate() != nil {
		return Denied(400, "invalid_gateway_settings")
	}
	if _, err := aigateway.Cost(aigateway.Usage{Input: 1, Output: 1}, route.Rates); err != nil ||
		route.Verified && route.Rates.Input+route.Rates.Output == 0 {
		return Denied(400, "invalid_model_rates")
	}
	if (in.Enabled || route.Enabled) && (!route.Verified || in.PlatformLimit == 0) {
		return Denied(409, "model_rates_not_verified")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var version int64
	if err = tx.QueryRow(ctx, `SELECT version FROM ai_gateway_settings WHERE singleton FOR UPDATE`).Scan(&version); err != nil {
		return err
	}
	if version != in.Version {
		return Denied(409, "version_conflict")
	}
	id := NewID()
	raw, _ := json.Marshal(GatewayProviderConfig{ModelConfig: route.Model, USDUSDTParity: route.USDUSDTParity})
	sealed, err := s.Vault.Seal("ai-gateway:"+id, raw)
	clear(raw)
	if err != nil {
		return err
	}
	var rateVersion int64
	err = tx.QueryRow(ctx, `SELECT coalesce(max(version),0)+1 FROM ai_model_rates WHERE alias=$1`, route.Alias).Scan(&rateVersion)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ai_model_rates(id,alias,version,provider_sealed,input_rate,output_rate,cache_read_rate,cache_write_rate,verified,created_by)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, route.Alias, rateVersion, sealed, route.Rates.Input, route.Rates.Output,
		route.Rates.CacheRead, route.Rates.CacheWrite, route.Verified, a.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ai_model_routes(alias,rate_id,enabled) VALUES($1,$2,$3)
        ON CONFLICT(alias) DO UPDATE SET rate_id=excluded.rate_id,enabled=excluded.enabled,version=ai_model_routes.version+1`,
		route.Alias, id, route.Enabled); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE ai_gateway_settings SET enabled=$1,platform_limit_micro=$2,test_limit_micro=$3,version=version+1`,
		in.Enabled, in.PlatformLimit, in.TestLimit); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,resource_id,detail)
        VALUES($1,'ai_gateway_configured',$2,jsonb_build_object('rates_verified',$3::boolean,'version',$4::bigint,'usd_usdt_parity',$5::boolean))`,
		a.ID, id, route.Verified, version+1, route.USDUSDTParity); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type gatewayRequest struct {
	Actor                   Actor
	Service                 *WorkspaceServiceContext
	ServiceTask             string
	HeatGrant               []byte
	Session                 SessionRef
	Group                   string
	Purpose, Key            string
	ExpectedRoute           int64
	Check                   func(context.Context) error
	ExpectedRateID          string
	MaxCost                 int64
	CallID                  *string
	ExpectedConsumerVersion int64
}

func (r gatewayRequest) scopeIDs() (tenant, actor string) {
	if r.Service != nil {
		return r.Service.TenantID, r.Service.OwnerID
	}
	return r.Actor.TenantID, r.Actor.ID
}

func (s *GatewayService) gatewayPermissions(ctx context.Context, tx pgx.Tx, r gatewayRequest) ([]string, error) {
	if r.Service == nil {
		return LiveActorPermissions(ctx, tx, r.Actor, s.Catalog)
	}
	if _, _, err := gatewayAccounting(r); err != nil {
		return nil, err
	}
	if err := s.heatServiceAdmission(ctx, tx, r); err != nil {
		return nil, err
	}
	return []string{"groups:ai"}, nil
}

type gatewayReservation struct {
	controlledExecutionFence
	ID, Period, RateID           string
	ExecutionMode, BillingSource string
	Amount                       int64
	Rates                        aigateway.Rates
	Model                        assistant.ModelConfig
}
type assistantGatewayProvider struct {
	s     *GatewayService
	h     *AssistantHost
	count int
}

func (s *GatewayService) AssistantProvider(h *AssistantHost) assistant.Provider {
	return &assistantGatewayProvider{s: s, h: h}
}
func (p *assistantGatewayProvider) Generate(ctx context.Context, messages []assistant.Message, tools []assistant.Tool, emit func(string) error) (assistant.Response, error) {
	p.count++
	if p.count > assistant.MaxModels {
		return assistant.Response{}, assistant.ErrLimit
	}
	a, err := p.h.service.actor(ctx, p.s.DB.Platform, p.h.ref, false)
	if err != nil {
		return assistant.Response{}, err
	}
	return p.s.generate(ctx, gatewayRequest{Actor: a, Session: p.h.ref, Group: p.h.session.Scope.GroupID, Purpose: "assistant",
		Key: fmt.Sprintf("assistant:%s:%d", p.h.turnID, p.count), Check: p.h.Check}, messages, tools, emit)
}
func (s *GatewayService) generate(ctx context.Context, request gatewayRequest, messages []assistant.Message, tools []assistant.Tool, emit func(string) error) (assistant.Response, error) {
	var out assistant.Response
	tenant, _ := request.scopeIDs()
	if tenant == "" {
		tenant = "platform-test"
		if request.Purpose == "platform_assistant" {
			tenant = "platform-assistant"
		}
	}
	release, err := s.Scheduler.Acquire(ctx, tenant)
	if err != nil {
		return out, Denied(429, "ai_gateway_busy")
	}
	defer release()
	if request.Check != nil {
		if err = request.Check(ctx); err != nil {
			return out, err
		}
	}
	raw, err := json.Marshal(struct {
		Messages []assistant.Message
		Tools    []assistant.Tool
	}{messages, tools})
	if err != nil || len(raw) > 128*1024 {
		return out, assistant.ErrLimit
	}
	reservation, err := s.reserve(ctx, request, raw)
	if err != nil {
		return out, err
	}
	if request.CallID != nil {
		*request.CallID = reservation.ID
	}
	finalize := func(usage assistant.Usage, submitted bool) error {
		work, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		return s.settle(work, reservation.ID, usage, submitted, false, "")
	}
	modelContext, cancel, err := reservation.controlledExecutionFence.modelContext(ctx, s.DB.Platform)
	if err != nil {
		_ = finalize(assistant.Usage{}, false)
		return out, err
	}
	ctx = modelContext
	defer cancel()
	if request.Check != nil {
		if err = request.Check(ctx); err != nil {
			_ = finalize(assistant.Usage{}, false)
			return out, err
		}
	}
	if err = ctx.Err(); err != nil {
		_ = finalize(assistant.Usage{}, false)
		return out, err
	}
	factory := s.ProviderFactory
	if factory == nil {
		factory = func(c assistant.ModelConfig) (assistant.Provider, error) { return assistant.NewHTTPProvider(c) }
	}
	provider, err := factory(reservation.Model)
	if err != nil {
		_ = finalize(assistant.Usage{}, false)
		return out, err
	}
	if request.Purpose == "test" {
		if bounded, ok := provider.(*assistant.HTTPProvider); ok {
			bounded.LimitOutput(32)
		}
	}
	if request.Purpose == "platform_assistant" {
		if bounded, ok := provider.(*assistant.HTTPProvider); ok {
			bounded.LimitOutput(512)
		}
	}
	if request.Check != nil {
		err = request.Check(ctx)
	}
	if err == nil {
		err = s.submit(ctx, request, reservation)
	}
	if err != nil {
		_ = finalize(assistant.Usage{}, false)
		return out, err
	}
	out, err = provider.Generate(ctx, messages, tools, emit)
	if settleErr := finalize(out.Usage, true); settleErr != nil {
		return out, settleErr
	}
	if err == nil && request.Check != nil {
		if err = request.Check(ctx); err != nil {
			return assistant.Response{}, err
		}
	}
	return out, err
}

// This short transaction is the submission boundary. No model I/O holds its locks.
func (s *GatewayService) submit(ctx context.Context, r gatewayRequest, reservation gatewayReservation) error {
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798460)`); err != nil {
		return err
	}
	grants, err := s.gatewayPermissions(ctx, tx, r)
	if err != nil {
		return err
	}
	permission := "assistant:use"
	if r.Purpose == "heatbot" {
		permission = "groups:ai"
	}
	if !slices.Contains(grants, permission) {
		return Denied(403, "permission_denied")
	}
	if r.Purpose == "platform_assistant" {
		if err = s.platformAssistantAdmission(ctx, tx, r, reservation.Amount); err != nil {
			return err
		}
	}
	if len(r.Session.hash) > 0 {
		var actor string
		if err = tx.QueryRow(ctx, `SELECT user_id::text FROM sessions WHERE token_hash=$1 AND user_id=$2
            AND expires_at>clock_timestamp() FOR SHARE`, r.Session.hash, r.Actor.ID).Scan(&actor); err != nil {
			if err == pgx.ErrNoRows {
				return Denied(401, "authentication_required")
			}
			return err
		}
	}
	mode, source, err := gatewayAccounting(r)
	if err != nil {
		return err
	}
	tenant, actor := r.scopeIDs()
	var storedMode, storedSource, storedActor, storedTenant, storedPurpose, storedTask, storedGroup, storedKey string
	err = tx.QueryRow(ctx, `SELECT execution_mode,billing_source,actor_id::text,
        coalesce(tenant_id::text,''),purpose,coalesce(service_task_id::text,''),coalesce(group_id::text,''),external_key
        FROM ai_calls WHERE id=$1 FOR SHARE`, reservation.ID).
		Scan(&storedMode, &storedSource, &storedActor, &storedTenant, &storedPurpose, &storedTask, &storedGroup, &storedKey)
	if err == pgx.ErrNoRows {
		return Denied(409, "ai_call_not_found")
	}
	if err != nil {
		return err
	}
	if storedMode != mode || storedSource != source || storedActor != actor ||
		storedTenant != tenant || storedPurpose != r.Purpose || storedTask != r.ServiceTask ||
		storedGroup != r.Group || storedKey != r.Key {
		return Denied(409, "ai_call_scope_changed")
	}
	var route string
	err = tx.QueryRow(ctx, `SELECT r.rate_id::text FROM ai_gateway_settings s CROSS JOIN ai_model_routes r
        JOIN ai_model_rates m ON m.id=r.rate_id WHERE s.singleton AND s.enabled AND r.alias='default'
        AND r.enabled AND m.verified AND r.rate_id=$1 FOR SHARE OF s,r`, reservation.RateID).Scan(&route)
	if err == pgx.ErrNoRows {
		return Denied(409, "model_route_changed")
	}
	if err != nil {
		return err
	}
	if reservation.Period != "" {
		var period string
		err = tx.QueryRow(ctx, `SELECT p.id::text FROM subscription_periods p JOIN subscriptions sub ON sub.tenant_id=p.tenant_id
            WHERE p.id=$1 AND p.tenant_id=$2 AND p.state='active' AND p.starts_at<=clock_timestamp()
            AND p.ends_at>clock_timestamp() AND sub.enabled AND sub.expires_at>clock_timestamp() FOR SHARE OF p,sub`,
			reservation.Period, tenant).Scan(&period)
		if err == pgx.ErrNoRows {
			return Denied(403, "active_credit_period_required")
		}
		if err != nil {
			return err
		}
	}
	if r.Group != "" {
		controlledActor := r.Actor.ID
		if r.Service != nil {
			controlledActor = ""
		}
		var platform string
		if err = tx.QueryRow(ctx, `SELECT platform FROM native_groups WHERE id=$1`, r.Group).Scan(&platform); err != nil {
			return err
		}
		if err = reservation.controlledExecutionFence.require(ctx, tx, controlledActor, platform, r.Group); err != nil {
			return err
		}
		if r.Service == nil {
			if err = RequireGroup(ctx, tx, r.Actor, r.Group); err != nil {
				return err
			}
		}
		var group string
		err = tx.QueryRow(ctx, `SELECT group_id::text FROM group_bindings WHERE group_id=$1 AND active
            AND ($2='' OR tenant_id=nullif($2,'')::uuid) FOR SHARE`, r.Group, tenant).Scan(&group)
		if err == pgx.ErrNoRows {
			return Denied(403, "group_not_found")
		}
		if err != nil {
			return err
		}
	}
	if err = reservation.controlledExecutionFence.requireSubmission(ctx, tx, 0); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE ai_calls SET status='submitted',submitted_at=clock_timestamp()
        WHERE id=$1 AND status='reserved'`, reservation.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return Denied(409, "ai_call_not_submittable")
	}
	return tx.Commit(ctx)
}
func (s *GatewayService) reserve(ctx context.Context, r gatewayRequest, raw []byte) (gatewayReservation, error) {
	var out gatewayReservation
	if !slices.Contains([]string{"assistant", "heatbot", "test", "platform_assistant"}, r.Purpose) {
		return out, Denied(400, "invalid_ai_purpose")
	}
	mode, source, err := gatewayAccounting(r)
	if err != nil {
		return out, err
	}
	tenant, actor := r.scopeIDs()
	if tenant != "" && source == "tenant_wallet" {
		if err := (&BillingService{DB: s.DB}).Refresh(ctx, tenant); err != nil {
			return out, err
		}
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	// All wallet, platform and sub-limit reservations share this brief lock.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798460)`); err != nil {
		return out, err
	}
	grants, err := s.gatewayPermissions(ctx, tx, r)
	if err != nil {
		return out, err
	}
	if len(r.Session.hash) > 0 {
		var ok bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE token_hash=$1 AND user_id=$2 AND expires_at>clock_timestamp())`, r.Session.hash, r.Actor.ID).Scan(&ok); err != nil {
			return out, err
		}
		if !ok {
			return out, Denied(401, "authentication_required")
		}
	}
	out.ExecutionMode, out.BillingSource = mode, source
	if r.Group != "" && r.Service == nil {
		if err = RequireGroup(ctx, tx, r.Actor, r.Group); err != nil {
			return out, err
		}
	}
	permission := "assistant:use"
	if r.Purpose == "heatbot" {
		permission = "groups:ai"
	}
	if !slices.Contains(grants, permission) {
		return out, Denied(403, "permission_denied")
	}
	if r.Purpose == "platform_assistant" {
		if err = s.platformAssistantAdmission(ctx, tx, r, 0); err != nil {
			return out, err
		}
		var busy bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_calls WHERE actor_id=$1
			AND purpose='platform_assistant' AND status IN ('reserved','submitted','unknown'))`, r.Actor.ID).Scan(&busy); err != nil {
			return out, err
		}
		if busy {
			return out, Denied(409, "platform_assistant_busy")
		}
	}
	if r.Purpose == "test" {
		if r.Actor.Role != "superadmin" || r.Actor.TenantID != "" {
			return out, Denied(403, "superadmin_required")
		}
		var busy bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_calls
			WHERE actor_id=$1 AND purpose='test' AND status IN ('reserved','submitted','unknown'))`, r.Actor.ID).Scan(&busy); err != nil {
			return out, err
		}
		if busy {
			return out, Denied(409, "gateway_test_unresolved")
		}
	}
	if r.Purpose == "heatbot" {
		var busy bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_calls WHERE group_id=$1 AND purpose='heatbot' AND status IN ('reserved','submitted'))`, r.Group).Scan(&busy); err != nil {
			return out, err
		}
		if busy {
			return out, Denied(409, "heat_group_busy")
		}
	}
	var enabled bool
	var platformLimit, testLimit int64
	err = tx.QueryRow(ctx, `SELECT enabled,platform_limit_micro,test_limit_micro FROM ai_gateway_settings FOR SHARE`).Scan(&enabled, &platformLimit, &testLimit)
	if err != nil {
		return out, err
	}
	if !enabled {
		return out, Denied(409, "ai_gateway_disabled")
	}
	var sealed []byte
	var routeVersion int64
	err = tx.QueryRow(ctx, `SELECT m.id::text,m.provider_sealed,m.input_rate,m.output_rate,m.cache_read_rate,m.cache_write_rate,r.version
        FROM ai_model_routes r JOIN ai_model_rates m ON m.id=r.rate_id WHERE r.alias='default' AND r.enabled AND m.verified FOR SHARE OF r`,
	).Scan(&out.RateID, &sealed, &out.Rates.Input, &out.Rates.Output, &out.Rates.CacheRead, &out.Rates.CacheWrite, &routeVersion)
	if err == pgx.ErrNoRows {
		return out, Denied(409, "model_not_verified")
	}
	if err != nil {
		return out, err
	}
	if r.ExpectedRoute > 0 && r.ExpectedRoute != routeVersion {
		return out, Denied(409, "model_route_changed")
	}
	if r.ExpectedRateID != "" && r.ExpectedRateID != out.RateID {
		return out, Denied(409, "model_route_changed")
	}
	plain, err := s.Vault.Open("ai-gateway:"+out.RateID, sealed)
	if err != nil {
		return out, err
	}
	defer clear(plain)
	if json.Unmarshal(plain, &out.Model) != nil {
		return out, Denied(503, "model_unavailable")
	}
	reserveRate := out.Rates
	reserveRate.Input = max(reserveRate.Input, reserveRate.CacheRead, reserveRate.CacheWrite)
	out.Amount, err = aigateway.Cost(aigateway.Usage{Input: int64(len(raw)), Output: assistant.MaxOutputTokens}, reserveRate)
	if err != nil {
		return out, err
	}
	out.Amount = max(out.Amount, 1)
	if r.MaxCost > 0 && out.Amount > r.MaxCost {
		return out, Denied(409, "test_call_budget_exhausted")
	}
	var used, testUsed int64
	err = tx.QueryRow(ctx, `SELECT coalesce(sum(CASE WHEN status='settled' THEN effective_charged_micro WHEN status<>'released' THEN reserved_micro ELSE 0 END),0)::bigint,
        coalesce(sum(CASE WHEN status='settled' THEN effective_charged_micro WHEN status<>'released' THEN reserved_micro ELSE 0 END)
            FILTER(WHERE tenant_id IS NULL AND purpose<>'platform_assistant'
                AND billing_source NOT IN('platform_demo','platform_workspace')),0)::bigint
        FROM ai_call_costs WHERE created_at>=date_trunc('month',clock_timestamp() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'`,
	).Scan(&used, &testUsed)
	if err != nil {
		return out, err
	}
	if err = gatewayBudgetAdmission(r, source, out.Amount, platformLimit-used, testLimit-testUsed); err != nil {
		return out, err
	}
	if tenant == "" {
		if r.Purpose == "platform_assistant" {
			if err = s.platformAssistantAdmission(ctx, tx, r, out.Amount); err != nil {
				return out, err
			}
		}
	} else if source == "tenant_wallet" {
		var remaining int64
		err = tx.QueryRow(ctx, `SELECT w.period_id::text,w.granted_micro-w.spent_micro-w.held_micro
            FROM ai_wallets w JOIN subscription_periods p ON p.id=w.period_id JOIN subscriptions sub ON sub.tenant_id=w.tenant_id
            WHERE w.tenant_id=$1 AND p.state='active' AND p.starts_at<=clock_timestamp() AND p.ends_at>clock_timestamp()
            AND sub.enabled AND sub.expires_at>clock_timestamp() FOR UPDATE OF w`, tenant).Scan(&out.Period, &remaining)
		if err == pgx.ErrNoRows {
			return out, Denied(409, "active_credit_period_required")
		}
		if err != nil {
			return out, err
		}
		if out.Amount > remaining {
			return out, Denied(409, "ai_credits_exhausted")
		}
		var capped bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_credit_limits l WHERE l.tenant_id=$1
            AND (l.kind='purpose' AND l.scope=$2 OR l.kind='member' AND l.scope=$3 OR l.kind='group' AND l.scope=$4)
            AND $5::bigint>l.limit_micro-coalesce((SELECT sum(CASE WHEN c.status='settled' THEN c.effective_charged_micro
                WHEN c.status<>'released' THEN c.reserved_micro ELSE 0 END) FROM ai_call_costs c WHERE c.period_id=$6
                AND (l.kind='purpose' AND c.purpose=l.scope OR l.kind='member' AND c.actor_id::text=l.scope
                    OR l.kind='group' AND c.group_id::text=l.scope)),0))`,
			tenant, r.Purpose, actor, r.Group, out.Amount, out.Period).Scan(&capped)
		if err != nil {
			return out, err
		}
		if capped {
			return out, Denied(409, "ai_credit_sub_limit")
		}
	}
	if r.Group != "" {
		controlledActor := r.Actor.ID
		if r.Service != nil {
			controlledActor = ""
		}
		var platform string
		if err = tx.QueryRow(ctx, `SELECT platform FROM native_groups WHERE id=$1`, r.Group).Scan(&platform); err != nil {
			return out, err
		}
		out.controlledExecutionFence, err = controlledExecutionScope(ctx, tx, controlledActor, platform, r.Group)
		if err != nil {
			return out, err
		}
		if err = ReserveControlledModelTx(ctx, tx, platform, r.Group, r.Key, controlledModelMicroUSDT(out.Amount)); err != nil {
			return out, err
		}
	}
	out.ID = NewID()
	tag, err := tx.Exec(ctx, `INSERT INTO ai_calls(id,tenant_id,actor_id,group_id,purpose,period_id,rate_id,
        external_key,request_hash,reserved_micro,execution_mode,billing_source,service_task_id)
        VALUES($1,nullif($2,'')::uuid,$3,nullif($4,'')::uuid,$5,nullif($6,'')::uuid,$7,$8,$9,$10,$11,$12,nullif($13,'')::uuid)
        ON CONFLICT DO NOTHING`,
		out.ID, tenant, actor, r.Group, r.Purpose, out.Period, out.RateID, r.Key,
		security.Digest(string(raw)), out.Amount, out.ExecutionMode, out.BillingSource, r.ServiceTask)
	if err != nil {
		return out, err
	}
	if tag.RowsAffected() != 1 {
		return out, Denied(409, "ai_call_already_recorded")
	}
	if r.Purpose == "platform_assistant" {
		tag, err = tx.Exec(ctx, `UPDATE platform_assistant_turns SET call_id=$1
			WHERE 'platform-assistant:'||id::text=$2 AND actor_id=$3 AND status='running' AND NOT cancel_requested`, out.ID, r.Key, r.Actor.ID)
		if err != nil {
			return out, err
		}
		if tag.RowsAffected() != 1 {
			return out, Denied(409, "platform_assistant_canceled")
		}
	}
	if out.Period != "" {
		if _, err = tx.Exec(ctx, `UPDATE ai_wallets SET held_micro=held_micro+$2 WHERE period_id=$1`, out.Period, out.Amount); err != nil {
			return out, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return out, nil
}

func gatewayBudgetAdmission(r gatewayRequest, source string, amount, platformRemaining, testRemaining int64) error {
	if amount > platformRemaining {
		return Denied(409, "platform_ai_budget_exhausted")
	}
	tenant, _ := r.scopeIDs()
	if tenant == "" && r.Purpose != "platform_assistant" && source != "platform_demo" &&
		(r.Actor.Role != "superadmin" || amount > testRemaining) {
		return Denied(409, "test_ai_budget_exhausted")
	}
	return nil
}

// gatewayAccounting is derived from the live server actor and the fixed
// consumer purpose. No request field or model output can select a billing
// identity.
func gatewayAccounting(r gatewayRequest) (string, string, error) {
	if r.Service != nil {
		service := r.Service
		if r.Purpose != "heatbot" || r.Actor != (Actor{}) || len(r.Session.hash) != 0 ||
			!ValidID(r.Group) || !ValidID(service.TenantID) || !ValidID(service.OwnerID) ||
			service.OwnerVersion < 1 || len(r.HeatGrant) != 32 ||
			r.ServiceTask != "" && (!ValidID(r.ServiceTask) || r.Key != "heat-job:"+r.ServiceTask) {
			return "", "", Denied(403, "heat_gateway_scope_changed")
		}
		if service.Demo && service.ownerRole == "superadmin" && ValidID(r.ServiceTask) &&
			service.ExecutionMode == "workspace_demo" && service.BillingSource == "platform_workspace" {
			return service.ExecutionMode, service.BillingSource, nil
		}
		if !service.Demo && service.ownerRole == "user" &&
			service.ExecutionMode == "tenant_business" && service.BillingSource == "tenant_wallet" {
			return service.ExecutionMode, service.BillingSource, nil
		}
		return "", "", Denied(403, "heat_gateway_scope_changed")
	}
	if r.ServiceTask != "" || len(r.HeatGrant) != 0 {
		return "", "", Denied(403, "heat_gateway_scope_changed")
	}
	switch r.Purpose {
	case "assistant":
		if r.Actor.Role == "superadmin" && r.Actor.TenantID == "" {
			if len(r.Session.hash) != 32 || !ValidID(r.Group) {
				return "", "", Denied(403, "assistant_scope_changed")
			}
			return "operator_demo", "platform_demo", nil
		}
		if r.Actor.Role == "user" && r.Actor.TenantID != "" {
			return "tenant_business", "tenant_wallet", nil
		}
		return "", "", Denied(403, "assistant_scope_changed")
	case "heatbot":
		return "", "", Denied(403, "heat_gateway_scope_changed")
	case "test":
		if r.Actor.Role == "superadmin" && r.Actor.TenantID == "" {
			return "legacy", "platform_test", nil
		}
		return "", "", Denied(403, "superadmin_required")
	case "platform_assistant":
		// This consumer predates the new accounting columns. Keep its
		// historical null-tenant semantics and platform-assistant admission.
		if r.Actor.Role == "superadmin" && r.Actor.TenantID == "" {
			return "legacy", "legacy", nil
		}
		return "", "", Denied(403, "superadmin_required")
	default:
		return "", "", Denied(400, "invalid_ai_purpose")
	}
}

// Usage exposes only the live website actor's own costs. Operator demo costs
// have no personal ceiling; every call still consumes the shared platform cap.
func (s *GatewayService) Usage(ctx context.Context, ref SessionRef) (map[string]any, error) {
	return s.usage(ctx, ref, false)
}

// UsageForCenter permits billing/operation readers to view their own costs
// without granting assistant access. Members retain their current group scope.
func (s *GatewayService) UsageForCenter(ctx context.Context, ref SessionRef) (map[string]any, error) {
	return s.usage(ctx, ref, true)
}

// Cost returns micro-credits: one credit is 0.001 USDT, not one USDT.
const gatewayUsageCurrencyScale int64 = 1000 * aigateway.MicroPerCredit

func (s *GatewayService) usage(ctx context.Context, ref SessionRef, center bool) (map[string]any, error) {
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	a, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return nil, err
	}
	grants, err := LiveActorPermissions(ctx, tx, a, s.Catalog)
	if err != nil {
		return nil, err
	}
	allowed := slices.Contains(grants, "assistant:use")
	if center {
		allowed = slices.Contains(grants, "billing:read") || slices.Contains(grants, "operations:read")
	}
	if !allowed {
		return nil, Denied(403, "permission_denied")
	}
	demo := a.Role == "superadmin" && a.TenantID == ""
	var month string
	var calls, settled, pending, spent, held int64
	err = tx.QueryRow(ctx, `SELECT to_char(clock_timestamp() AT TIME ZONE 'Asia/Shanghai','YYYY-MM'),
        count(*)::bigint,count(*) FILTER(WHERE status='settled')::bigint,
        count(*) FILTER(WHERE status IN ('reserved','submitted','unknown'))::bigint,
        coalesce(sum(effective_charged_micro) FILTER(WHERE status='settled'),0)::bigint,
        coalesce(sum(reserved_micro) FILTER(WHERE status IN ('reserved','submitted','unknown')),0)::bigint
        FROM ai_call_costs c WHERE actor_id=$1 AND (
            $2::boolean AND tenant_id IS NULL AND period_id IS NULL AND purpose='assistant'
                AND execution_mode='operator_demo' AND billing_source='platform_demo'
                AND created_at>=date_trunc('month',clock_timestamp() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'
            OR NOT $2::boolean AND tenant_id=nullif($3,'')::uuid
                AND period_id IN (SELECT id FROM subscription_periods
                    WHERE tenant_id=nullif($3,'')::uuid AND state='active' AND ends_at>clock_timestamp())
                AND (NOT $4::boolean OR group_id IS NULL OR EXISTS(
                    SELECT 1 FROM group_bindings b WHERE b.group_id=c.group_id AND b.tenant_id=c.tenant_id
                    AND b.active AND member_group_visible(b.tenant_id,b.group_id,$1::uuid)))
        )`, a.ID, demo, a.TenantID, center).Scan(&month, &calls, &settled, &pending, &spent, &held)
	if err != nil {
		return nil, err
	}
	var background map[string]any
	if demo {
		space, e := OperatorWorkspace(ctx, tx, a.ID)
		if e != nil {
			return nil, e
		}
		var calls, settled, pending, spent, held int64
		e = tx.QueryRow(ctx, `SELECT count(*)::bigint,count(*) FILTER(WHERE status='settled')::bigint,
			count(*) FILTER(WHERE status IN('reserved','submitted','unknown'))::bigint,
			coalesce(sum(effective_charged_micro) FILTER(WHERE status='settled'),0)::bigint,
			coalesce(sum(reserved_micro) FILTER(WHERE status IN('reserved','submitted','unknown')),0)::bigint
			FROM ai_call_costs WHERE tenant_id=$1 AND actor_id=$2 AND purpose='heatbot'
			AND execution_mode='workspace_demo' AND billing_source='platform_workspace' AND period_id IS NULL
			AND service_task_id IS NOT NULL
			AND created_at>=date_trunc('month',clock_timestamp() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'`,
			space.ID, a.ID).Scan(&calls, &settled, &pending, &spent, &held)
		if e != nil {
			return nil, e
		}
		background = map[string]any{
			"tenant_id": space.ID, "execution_mode": "workspace_demo", "billing_source": "platform_workspace",
			"call_count": calls, "settled_call_count": settled, "pending_call_count": pending,
			"actual_micro": spent, "held_micro": held, "used_or_reserved": spent + held,
			"unit": "micro_credit", "currency": "USDT", "currency_scale": gatewayUsageCurrencyScale,
			"limit": nil, "unlimited": true,
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	var limit any = int64(0) // Retain the legacy customer gateway field.
	mode, source := "tenant_business", "tenant_wallet"
	if demo {
		limit = nil
		mode, source = "operator_demo", "platform_demo"
	}
	return map[string]any{
		"month": month, "timezone": "Asia/Shanghai", "unit": "micro_credit",
		"currency": "USDT", "currency_scale": gatewayUsageCurrencyScale,
		"call_count": calls, "settled_call_count": settled, "pending_call_count": pending,
		"actual_micro": spent, "held_micro": held, "used_or_reserved": spent + held,
		"unverified_reservations": held, "limit": limit, "unlimited": demo,
		"execution_mode": mode, "billing_source": source,
		"background_usage": background,
	}, nil
}

func (s *GatewayService) settle(ctx context.Context, id string, usage assistant.Usage, submitted, reconcile bool, reason string) error {
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798460)`); err != nil {
		return err
	}
	var state, tenant, period, actor string
	var held int64
	var rates aigateway.Rates
	var previousUsage []byte
	err = tx.QueryRow(ctx, `SELECT c.status,coalesce(c.tenant_id::text,''),coalesce(c.period_id::text,''),c.actor_id::text,c.reserved_micro,c.usage,
        r.input_rate,r.output_rate,r.cache_read_rate,r.cache_write_rate FROM ai_calls c JOIN ai_model_rates r ON r.id=c.rate_id WHERE c.id=$1 FOR UPDATE OF c`,
		id).Scan(&state, &tenant, &period, &actor, &held, &previousUsage, &rates.Input, &rates.Output, &rates.CacheRead, &rates.CacheWrite)
	if err != nil {
		return err
	}
	if state == "settled" || state == "released" {
		if reconcile {
			var previous assistant.Usage
			if state != "settled" || json.Unmarshal(previousUsage, &previous) != nil || previous != usage {
				return Denied(409, "idempotency_conflict")
			}
		}
		return nil
	}
	if reconcile && state != "unknown" {
		return Denied(409, "ai_call_not_unknown")
	}
	charged := int64(0)
	next := "released"
	if submitted {
		next = "unknown"
		if usage.Valid() {
			charged, err = aigateway.Cost(aigateway.Usage{Input: usage.PromptTokens, Output: usage.CompletionTokens,
				CacheRead: usage.PromptDetails.CachedTokens, CacheWrite: usage.PromptDetails.CacheWriteTokens}, rates)
			if err != nil {
				return err
			}
			next = "settled"
		}
	}
	if next == "unknown" {
		_, err = tx.Exec(ctx, `UPDATE ai_calls SET status='unknown',finished_at=clock_timestamp() WHERE id=$1`, id)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if charged > held { // Provider exceeded the conservative reservation: freeze for explicit reconciliation.
		if !reconcile {
			_, err = tx.Exec(ctx, `UPDATE ai_calls SET status='unknown' WHERE id=$1`, id)
			if err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
	}
	if period != "" {
		tag, e := tx.Exec(ctx, `UPDATE ai_wallets SET held_micro=held_micro-$2,spent_micro=spent_micro+$3
            WHERE period_id=$1 AND granted_micro-spent_micro-held_micro+$2 >=$3`, period, held, charged)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return Denied(409, "ai_reconciliation_budget_conflict")
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE ai_calls SET status=$2,charged_micro=$3,usage=$4,finished_at=clock_timestamp() WHERE id=$1`,
		id, next, charged, usage); err != nil {
		return err
	}
	kind := "charge"
	amount := -charged
	if next == "released" {
		kind = "release"
		amount = held
	}
	if _, err = tx.Exec(ctx, `INSERT INTO credit_entries(tenant_id,period_id,call_id,actor_id,kind,amount_micro,reason,external_key)
        VALUES(nullif($1,'')::uuid,nullif($2,'')::uuid,$3,$4,$5,$6,$7,$8)`,
		tenant, period, id, actor, kind, amount, reason, "call:"+id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *GatewayService) Recover(ctx context.Context) error {
	rows, err := s.DB.Platform.Query(ctx, `SELECT id::text,status FROM ai_calls WHERE status IN ('reserved','submitted')`)
	if err != nil {
		return err
	}
	type item struct{ id, state string }
	var calls []item
	for rows.Next() {
		var c item
		if err = rows.Scan(&c.id, &c.state); err != nil {
			break
		}
		calls = append(calls, c)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, c := range calls {
		if err = s.settle(ctx, c.id, assistant.Usage{}, c.state == "submitted", false, "process_restarted"); err != nil {
			return err
		}
	}
	if _, err = s.DB.Platform.Exec(ctx, `UPDATE platform_assistant_turns t SET
		call_id=c.id,status=CASE WHEN c.status='unknown' THEN 'unknown' ELSE 'canceled' END,finished_at=clock_timestamp()
		FROM ai_calls c WHERE t.status='running' AND c.external_key='platform-assistant:'||t.id::text`); err != nil {
		return err
	}
	if _, err = s.DB.Platform.Exec(ctx, `UPDATE platform_assistant_turns SET status='canceled',finished_at=clock_timestamp()
		WHERE status='running'`); err != nil {
		return err
	}
	return s.PruneGrants(ctx)
}
func (s *GatewayService) Reconcile(ctx context.Context, a Actor, id string, usage assistant.Usage, reason string) error {
	if a.Role != "superadmin" {
		return Denied(403, "superadmin_required")
	}
	if !usage.Valid() || len(reason) < 3 || len(reason) > 200 {
		return Denied(400, "verified_usage_required")
	}
	return s.settle(ctx, id, usage, true, true, reason)
}
