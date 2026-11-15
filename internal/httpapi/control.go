package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/control"
	"wangshangliao/internal/features"
	"wangshangliao/internal/security"
)

var supportedModules = map[string]bool{
	"moderation": true, "replies": true, "schedules": true, "rankings": true,
	"cards": true, "friends": true, "activities": true, "finance": true, "ai": true,
	"management_assistant": true,
}

func (s *Server) accounts(w http.ResponseWriter, r *http.Request, p Principal) error {
	columns := `id,name,workspace,platform,purpose,native_uid,connection_enabled,testing_enabled,
        runtime_state,epoch,version,created_at,auth_state,authenticated_at,native_nickname`
	if p.Role == "superadmin" {
		columns += `,
        (SELECT state FROM native_signup_attempts ns WHERE ns.account_id=accounts.id AND state IN ('submitted','registered')) AS signup_state,
        (SELECT registered_account FROM native_signup_attempts ns WHERE ns.account_id=accounts.id AND state='registered') AS registered_account`
	}
	return s.list(w, r, p, `SELECT `+columns+` FROM accounts WHERE workspace='`+requestWorkspace(r)+`' ORDER BY created_at LIMIT 10`)
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 {
		return fail(400, "invalid_name")
	}
	if in.Platform == "" {
		in.Platform = "wsl"
		if requestWorkspace(r) == "lajiaoliao" {
			in.Platform = "lajiaoliao"
		}
	}
	if (in.Platform != "wsl" && in.Platform != "lajiaoliao") ||
		(requestWorkspace(r) != "maibot" && in.Platform != requestWorkspace(r)) {
		return fail(400, "invalid_account_scope")
	}
	purpose := "management"
	if requestWorkspace(r) == "maibot" {
		purpose = "chat"
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(798422)`); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM accounts WHERE purpose=$1`, purpose).Scan(&count); err != nil {
		return err
	}
	if count >= control.MaxAccountsPerWorkspace {
		return fail(409, "account_limit_reached")
	}
	var id string
	if err = tx.QueryRow(r.Context(), `INSERT INTO accounts(name,workspace,platform,purpose) VALUES($1,$2,$3,$4) RETURNING id::text`,
		strings.TrimSpace(in.Name), requestWorkspace(r), in.Platform, purpose).Scan(&id); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, "", "account_created", id,
		map[string]string{"name": in.Name, "workspace": requestWorkspace(r)}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 201, map[string]string{"id": id})
	return nil
}

func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Name              *string `json:"name"`
		ConnectionEnabled *bool   `json:"connection_enabled"`
		TestingEnabled    *bool   `json:"testing_enabled"`
		Version           int64   `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Version < 1 || (in.Name != nil && (strings.TrimSpace(*in.Name) == "" || len(*in.Name) > 100)) {
		return fail(400, "invalid_account")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if in.ConnectionEnabled != nil && *in.ConnectionEnabled {
		if in.TestingEnabled != nil && !*in.TestingEnabled {
			return fail(409, "native_compatibility_not_verified")
		}
		if err = control.RequireTestConnectionTx(r.Context(), tx, assistantRef(r), r.PathValue("id"), in.Version); err != nil {
			return err
		}
	}
	// The current native connection switch admits tests only. Leaving test mode
	// must not retain that admission as a production connection.
	if in.TestingEnabled != nil && !*in.TestingEnabled {
		disabled := false
		in.ConnectionEnabled = &disabled
	}
	var version int64
	err = tx.QueryRow(r.Context(), `UPDATE accounts SET name=coalesce($1,name),
        connection_enabled=coalesce($2,connection_enabled),testing_enabled=coalesce($3,testing_enabled),
        epoch=epoch+1,version=version+1 WHERE id=$4 AND version=$5 RETURNING version`,
		in.Name, in.ConnectionEnabled, in.TestingEnabled, r.PathValue("id"), in.Version).Scan(&version)
	if err == pgx.ErrNoRows {
		return fail(409, "version_conflict")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE operations SET status='cancelled',result_code='account_changed',updated_at=now()
        WHERE account_id=$1 AND status='queued'`, r.PathValue("id")); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, "", "account_updated", r.PathValue("id"), in); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	if s.GroupBinding != nil {
		if err = s.GroupBinding.SweepControlledScopes(r.Context()); err != nil {
			return err
		}
	}
	send(w, 200, map[string]int64{"version": version})
	return nil
}

func (s *Server) plans(w http.ResponseWriter, r *http.Request, p Principal) error {
	return s.list(w, r, p, `SELECT * FROM plans ORDER BY name LIMIT 100`)
}

func (s *Server) createPlan(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Name                   string   `json:"name"`
		GroupLimit             int      `json:"group_limit"`
		AIGroupLimit           int      `json:"ai_group_limit"`
		MonthlyAITokens        int64    `json:"monthly_ai_tokens"`
		MonthlyAssistantTokens int64    `json:"monthly_assistant_tokens"`
		Modules                []string `json:"modules"`
		AllowedPlatforms       []string `json:"allowed_platforms"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 || in.GroupLimit < 0 || in.GroupLimit > control.MaxAuthorizedGroups ||
		in.AIGroupLimit < 0 || in.AIGroupLimit > control.MaxAuthorizedGroups || in.AIGroupLimit > in.GroupLimit || in.MonthlyAITokens < 0 ||
		in.MonthlyAssistantTokens < 0 || in.MonthlyAssistantTokens > 1e12 {
		return fail(400, "invalid_plan")
	}
	for _, module := range in.Modules {
		if !supportedModules[module] {
			return fail(400, "unsupported_module")
		}
	}
	if in.Modules == nil {
		in.Modules = []string{}
	}
	if len(in.AllowedPlatforms) == 0 {
		in.AllowedPlatforms = []string{"wsl"}
	}
	if len(in.AllowedPlatforms) > 2 {
		return fail(400, "invalid_platforms")
	}
	seen := map[string]bool{}
	for _, nativePlatform := range in.AllowedPlatforms {
		if (nativePlatform != "wsl" && nativePlatform != "lajiaoliao") || seen[nativePlatform] {
			return fail(400, "invalid_platforms")
		}
		seen[nativePlatform] = true
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var id string
	if err = tx.QueryRow(r.Context(), `INSERT INTO plans(name,group_limit,ai_group_limit,monthly_ai_tokens,modules,monthly_assistant_tokens,allowed_platforms)
        VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text`, in.Name, in.GroupLimit, in.AIGroupLimit, in.MonthlyAITokens, in.Modules, in.MonthlyAssistantTokens, in.AllowedPlatforms).Scan(&id); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, "", "plan_created", id, in); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 201, map[string]string{"id": id})
	return nil
}

func (s *Server) subscription(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		PlanID    string    `json:"plan_id"`
		ExpiresAt time.Time `json:"expires_at"`
		Enabled   bool      `json:"enabled"`
		Version   int64     `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !validUUID(in.PlanID) || in.ExpiresAt.IsZero() || (in.Enabled && !in.ExpiresAt.After(time.Now())) || in.Version < 0 {
		return fail(400, "invalid_subscription")
	}
	var commercial bool
	if err := s.DB.Platform.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM commercial_plans WHERE plan_id=$1)
        OR EXISTS(SELECT 1 FROM subscription_periods WHERE tenant_id=$2)`, in.PlanID, r.PathValue("id")).Scan(&commercial); err != nil {
		return err
	}
	if commercial {
		return fail(409, "billing_confirmation_required")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, r.PathValue("id")); err != nil {
		return err
	}
	var groupLimit, aiLimit, groups, aiGroups int
	err = tx.QueryRow(r.Context(), `SELECT group_limit,ai_group_limit FROM plans WHERE id=$1`, in.PlanID).Scan(&groupLimit, &aiLimit)
	if err == pgx.ErrNoRows {
		return fail(404, "plan_not_found")
	}
	if err != nil {
		return err
	}
	if err = tx.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER(WHERE ai_enabled OR ai_requested)
        FROM group_bindings WHERE tenant_id=$1 AND active`, r.PathValue("id")).Scan(&groups, &aiGroups); err != nil {
		return err
	}
	if in.Enabled && (groups > groupLimit || aiGroups > aiLimit) {
		return fail(409, "subscription_below_current_usage")
	}
	var excluded bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
        JOIN plans p ON p.id=$2 WHERE b.tenant_id=$1 AND b.active AND NOT g.platform=ANY(p.allowed_platforms))`,
		r.PathValue("id"), in.PlanID).Scan(&excluded); err != nil {
		return err
	}
	if in.Enabled && excluded {
		return fail(409, "subscription_below_current_platform_usage")
	}
	var version int64
	err = tx.QueryRow(r.Context(), `INSERT INTO subscriptions(tenant_id,plan_id,expires_at,enabled)
        SELECT $1,$2,$3,$4 WHERE $5::bigint=0
        ON CONFLICT(tenant_id) DO UPDATE SET plan_id=excluded.plan_id,expires_at=excluded.expires_at,
        enabled=excluded.enabled,version=subscriptions.version+1 WHERE subscriptions.version=$5
        RETURNING version`, r.PathValue("id"), in.PlanID, in.ExpiresAt, in.Enabled, in.Version).Scan(&version)
	// Existing subscriptions must be updated separately when version is nonzero.
	if err == pgx.ErrNoRows && in.Version > 0 {
		err = tx.QueryRow(r.Context(), `UPDATE subscriptions SET plan_id=$1,expires_at=$2,enabled=$3,version=version+1
            WHERE tenant_id=$4 AND version=$5 RETURNING version`, in.PlanID, in.ExpiresAt, in.Enabled, r.PathValue("id"), in.Version).Scan(&version)
	}
	if err == pgx.ErrNoRows {
		return fail(409, "version_conflict")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE group_bindings SET epoch=epoch+1,version=version+1 WHERE tenant_id=$1`, r.PathValue("id")); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE operations SET status='cancelled',result_code='subscription_changed',updated_at=now()
        WHERE tenant_id=$1 AND status='queued'`, r.PathValue("id")); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, r.PathValue("id"), "subscription_updated", r.PathValue("id"), in); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]int64{"version": version})
	return nil
}

func (s *Server) groups(w http.ResponseWriter, r *http.Request, p Principal) error {
	if requestWorkspace(r) == "maibot" {
		return s.list(w, r, p, `SELECT b.group_id,b.tenant_id,b.active,b.ai_account_id,b.ai_enabled,b.ai_requested,b.ai_request_state,
            g.name,g.native_id,g.platform,coalesce(nullif(a.native_nickname,''),a.name) account_name,b.version FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
            JOIN accounts a ON a.id=b.ai_account_id AND a.workspace='maibot'
			LEFT JOIN group_binding_ownership ownership ON ownership.group_id=b.group_id
				AND ownership.tenant_id=b.tenant_id AND ownership.platform=g.platform
			WHERE coalesce(ownership.proof_kind,'authenticated_message')<>'controlled_test'
				OR ownership.state='active' AND EXISTS(SELECT 1 FROM onboarding_test_scopes cs
					WHERE cs.id=ownership.controlled_scope_id AND cs.actor_id=$1::uuid AND cs.group_id=b.group_id
					AND cs.tenant_id=b.tenant_id AND cs.platform=g.platform
					AND cs.account_id=b.primary_account_id AND onboarding_test_scope_active(cs.id))
			ORDER BY g.name LIMIT 300`, p.ID)
	}
	query := `SELECT b.*,g.name,g.native_id,g.platform,coalesce(nullif(a.native_nickname,''),a.name,'机器人已删除') account_name,
		coalesce(nullif(chat.native_nickname,''),chat.name,'') ai_account_name
        FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
        LEFT JOIN accounts a ON a.id=b.primary_account_id
		LEFT JOIN accounts chat ON chat.id=b.ai_account_id`
	if p.Role != "superadmin" {
		// Tenant RLS excludes operator spaces: their owner-only membership is
		// enforced by provision_operator_workspace, without exposing proof tables.
		return s.list(w, r, p, query+` WHERE g.platform=$1 ORDER BY g.name LIMIT 300`, requestWorkspace(r))
	}
	return s.list(w, r, p, query+` LEFT JOIN group_binding_ownership ownership ON ownership.group_id=b.group_id
			AND ownership.tenant_id=b.tenant_id AND ownership.platform=g.platform
		WHERE g.platform=$1 AND (coalesce(ownership.proof_kind,'authenticated_message')<>'controlled_test'
			OR ownership.state='active' AND EXISTS(SELECT 1 FROM onboarding_test_scopes cs
				WHERE cs.id=ownership.controlled_scope_id AND cs.actor_id=$2::uuid AND cs.group_id=b.group_id
				AND cs.tenant_id=b.tenant_id AND cs.platform=g.platform
				AND cs.account_id=b.primary_account_id AND onboarding_test_scope_active(cs.id)))
		ORDER BY g.name LIMIT 300`, requestWorkspace(r), p.ID)
}

// GroupConfig contains no management chat commands or unbounded regular expressions.
type GroupConfig = features.Config

func (s *Server) updateGroup(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Version           int64       `json:"version"`
		ManagementEnabled bool        `json:"management_enabled"`
		AIEnabled         bool        `json:"ai_enabled"`
		Config            GroupConfig `json:"config"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Version < 1 || len(in.Config.BlockedWords) > 200 || len(in.Config.Blacklist) > 2000 ||
		len(in.Config.Exemptions) > 2000 || len(in.Config.KeywordReplies) > 100 ||
		in.Config.AICooldownSeconds < 0 || in.Config.AICooldownSeconds > 86400 {
		return fail(400, "invalid_group_config")
	}
	for _, word := range in.Config.BlockedWords {
		if strings.TrimSpace(word) == "" || len(word) > 200 {
			return fail(400, "invalid_rule")
		}
	}
	for _, rule := range in.Config.KeywordReplies {
		if strings.TrimSpace(rule.Keyword) == "" || len(rule.Keyword) > 200 || len(rule.Reply) > 4000 {
			return fail(400, "invalid_rule")
		}
	}
	if in.AIEnabled {
		return fail(409, "ai_isolation_not_verified")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var tenant string
	var stored []byte
	var currentVersion int64
	var detached bool
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return err
	}
	err = tx.QueryRow(r.Context(), `SELECT tenant_id::text,config,version,primary_account_id IS NULL FROM group_bindings
        WHERE group_id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&tenant, &stored, &currentVersion, &detached)
	if err == pgx.ErrNoRows || (err == nil && p.Role != "superadmin" && tenant != p.TenantID) {
		return fail(404, "group_not_found")
	}
	if err != nil {
		return err
	}
	if detached {
		return fail(409, "group_robot_removed")
	}
	if in.Version != currentVersion {
		return fail(409, "version_conflict")
	}
	var platform string
	if err = tx.QueryRow(r.Context(), `SELECT platform FROM native_groups WHERE id=$1`,
		r.PathValue("id")).Scan(&platform); err != nil {
		return err
	}
	if err = control.RequireControlledBinding(r.Context(), tx, p.ID, platform, r.PathValue("id")); err != nil {
		return err
	}
	if in.ManagementEnabled {
		var pendingLeave bool
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM native_setting_operations
            WHERE group_id=$1 AND kind='group_leave' AND status IN('preparing','submitting','unknown'))`,
			r.PathValue("id")).Scan(&pendingLeave); err != nil {
			return err
		}
		if pendingLeave {
			return fail(409, "native_setting_pending")
		}
	}
	var previous GroupConfig
	if err = json.Unmarshal(stored, &previous); err != nil {
		return err
	}
	// Legacy callers cannot remove or bypass separately permissioned switches.
	if in.Config.FeatureOptions != nil {
		before, _ := json.Marshal(previous.FeatureOptions)
		after, _ := json.Marshal(in.Config.FeatureOptions)
		if string(before) != string(after) {
			return fail(400, "use_feature_settings")
		}
	}
	in.Config.FeatureOptions = previous.FeatureOptions
	// Older full-config forms do not own the separately permissioned combinations.
	if in.Config.BlockedRules != nil {
		before, _ := json.Marshal(previous.BlockedRules)
		after, _ := json.Marshal(in.Config.BlockedRules)
		if !bytes.Equal(before, after) {
			return fail(400, "use_feature_settings")
		}
	}
	in.Config.BlockedRules = previous.BlockedRules
	if features.Validate(in.Config) != nil {
		return fail(400, "invalid_group_config")
	}
	var modules []string
	err = tx.QueryRow(r.Context(), `SELECT p.modules FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
        WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>clock_timestamp() FOR SHARE OF sub,p`, tenant).Scan(&modules)
	if err == pgx.ErrNoRows {
		return fail(403, "active_subscription_required")
	}
	if err != nil {
		return err
	}
	if in.ManagementEnabled && !slices.ContainsFunc(modules, func(module string) bool {
		return slices.Contains([]string{"moderation", "replies", "schedules", "rankings", "cards", "friends"}, module)
	}) {
		return fail(403, "module_not_in_plan")
	}
	required := map[string]string{}
	if !slices.Equal(previous.BlockedWords, in.Config.BlockedWords) ||
		!slices.Equal(previous.Blacklist, in.Config.Blacklist) || !slices.Equal(previous.Exemptions, in.Config.Exemptions) {
		required["moderation"] = "groups:moderation"
	}
	if !slices.Equal(previous.KeywordReplies, in.Config.KeywordReplies) {
		required["replies"] = "groups:replies"
	}
	if previous.AICooldownSeconds != in.Config.AICooldownSeconds {
		required["ai"] = "groups:ai"
	}
	actor := control.Actor{ID: p.ID, TenantID: p.TenantID, Role: p.Role}
	var liveActor string
	if err = tx.QueryRow(r.Context(), `SELECT id::text FROM users WHERE id=$1 AND role=$2 AND NOT disabled
        AND coalesce(tenant_id::text,'')=$3 FOR SHARE`, p.ID, p.Role, p.TenantID).Scan(&liveActor); err == pgx.ErrNoRows {
		return fail(403, "permission_denied")
	} else if err != nil {
		return err
	}
	permissions, err := control.EffectivePermissions(r.Context(), tx, actor, tenantPermissions)
	if err != nil {
		return err
	}
	for module, permission := range required {
		if !slices.Contains(modules, module) {
			return fail(403, "module_not_in_plan")
		}
		if !slices.Contains(permissions, permission) {
			return fail(403, "permission_denied")
		}
	}
	changes := control.FeatureChanges{}
	addChange := func(id, field string, value any) {
		if changes[id] == nil {
			changes[id] = map[string]json.RawMessage{}
		}
		changes[id][field], _ = json.Marshal(value)
	}
	if !slices.Equal(previous.BlockedWords, in.Config.BlockedWords) {
		addChange("blocked_words", "words", in.Config.BlockedWords)
	}
	if !slices.Equal(previous.Blacklist, in.Config.Blacklist) {
		addChange("blacklist", "members", in.Config.Blacklist)
	}
	if !slices.Equal(previous.Exemptions, in.Config.Exemptions) {
		addChange("exemptions", "members", in.Config.Exemptions)
	}
	if !slices.Equal(previous.KeywordReplies, in.Config.KeywordReplies) {
		addChange("keyword_replies", "rules", in.Config.KeywordReplies)
	}
	if len(changes) > 0 {
		if _, _, err = control.ValidateFeatures(previous, changes, actor, permissions, modules, true); err != nil {
			return err
		}
	}
	if in.ManagementEnabled {
		var eligible bool
		err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM group_bindings b
            JOIN accounts a ON a.id=b.primary_account_id
            JOIN account_groups ag ON ag.account_id=a.id AND ag.group_id=b.group_id
            JOIN subscriptions s ON s.tenant_id=b.tenant_id
            WHERE b.group_id=$1 AND b.active AND a.connection_enabled AND s.enabled AND s.expires_at>now()
            AND ag.role IN ('admin','owner') AND ag.observation_source='native' AND ag.observed_at>now()-interval '60 seconds')`, r.PathValue("id")).Scan(&eligible)
		if err != nil {
			return err
		}
		if !eligible {
			return fail(409, "fresh_native_role_required")
		}
	}
	result, err := control.CommitGroupConfigTx(r.Context(), tx, actor, tenant, r.PathValue("id"), in.Version, in.Config,
		&in.ManagementEnabled, &in.AIEnabled, "group_updated", map[string]any{"management_enabled": in.ManagementEnabled, "ai_enabled": in.AIEnabled})
	if err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]int64{"version": result.Version})
	return nil
}

func (s *Server) personas(w http.ResponseWriter, r *http.Request, p Principal) error {
	return s.list(w, r, p, `SELECT p.*,a.name account_name,a.native_nickname,a.platform FROM personas p JOIN accounts a ON a.id=p.account_id
        WHERE a.workspace='maibot' ORDER BY a.name LIMIT 10`)
}

func (s *Server) savePersona(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Nickname    string `json:"nickname"`
		Personality string `json:"personality"`
		ReplyStyle  string `json:"reply_style"`
		Version     int64  `json:"version"`
		Published   bool   `json:"published"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Nickname) == "" || len(in.Nickname) > 100 || len(in.Personality) > 12000 || len(in.ReplyStyle) > 4000 || in.Version < 0 {
		return fail(400, "invalid_persona")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var version int64
	if in.Version == 0 {
		err = tx.QueryRow(r.Context(), `INSERT INTO personas(account_id,nickname,personality,reply_style,published) VALUES($1,$2,$3,$4,$5)
            ON CONFLICT DO NOTHING RETURNING version`, r.PathValue("id"), in.Nickname, in.Personality, in.ReplyStyle, in.Published).Scan(&version)
	} else {
		err = tx.QueryRow(r.Context(), `UPDATE personas SET nickname=$1,personality=$2,reply_style=$3,version=version+1,published=$6
            WHERE account_id=$4 AND version=$5 RETURNING version`, in.Nickname, in.Personality, in.ReplyStyle, r.PathValue("id"), in.Version, in.Published).Scan(&version)
	}
	if err == pgx.ErrNoRows {
		return fail(409, "version_conflict")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE group_bindings SET epoch=epoch+1,version=version+1 WHERE ai_account_id=$1`, r.PathValue("id")); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, "", "persona_updated", r.PathValue("id"), map[string]int64{"version": version}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]int64{"version": version})
	return nil
}

func (s *Server) claims(w http.ResponseWriter, r *http.Request, p Principal) error {
	return s.list(w, r, p, `SELECT c.*,g.name group_name,g.native_id,g.platform FROM group_claims c JOIN native_groups g ON g.id=c.group_id
        WHERE g.platform=$1 ORDER BY c.created_at DESC LIMIT 300`, requestWorkspace(r))
}

func (s *Server) createClaim(w http.ResponseWriter, r *http.Request, p Principal) error {
	if p.Role == "superadmin" {
		return fail(400, "tenant_user_required")
	}
	var in struct {
		Code           string `json:"code"`
		NativeIdentity string `json:"native_identity"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if len(in.Code) != 43 || strings.TrimSpace(in.NativeIdentity) == "" || len(in.NativeIdentity) > 200 {
		return fail(400, "invalid_claim")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if err = control.RequireTenantOwner(r.Context(), tx, control.Actor{ID: p.ID, TenantID: p.TenantID, Role: p.Role}); err != nil {
		return err
	}
	var group, account string
	err = tx.QueryRow(r.Context(), `UPDATE claim_codes c SET consumed_at=now() FROM accounts a
        WHERE c.account_id=a.id AND a.purpose='management' AND a.platform=$2 AND a.connection_enabled AND a.epoch=c.account_epoch
        AND c.code_hash=$1 AND c.consumed_at IS NULL AND c.expires_at>now()
		AND (c.target_tenant_id IS NULL OR c.target_tenant_id=$3)
        RETURNING c.group_id::text,c.account_id::text`, security.Digest(in.Code), requestWorkspace(r), p.TenantID).Scan(&group, &account)
	if err == pgx.ErrNoRows {
		return fail(400, "code_invalid_or_expired")
	}
	if err != nil {
		return err
	}
	var id string
	if err = tx.QueryRow(r.Context(), `INSERT INTO group_claims(tenant_id,group_id,account_id,applicant_id,native_identity)
        VALUES($1,$2,$3,$4,$5) RETURNING id::text`, p.TenantID, group, account, p.ID, in.NativeIdentity).Scan(&id); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, p.TenantID, "claim_submitted", id, map[string]string{"group_id": group}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 201, map[string]string{"id": id, "state": "pending"})
	return nil
}

func (s *Server) reviewClaim(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Approve          bool   `json:"approve"`
		IdentityVerified bool   `json:"identity_verified"`
		Note             string `json:"note"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Note) == "" || len(in.Note) > 2000 || (in.Approve && !in.IdentityVerified) {
		return fail(400, "identity_review_required")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	// Global capacity and per-tenant quotas must be checked under the same lock.
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return err
	}
	var tenant, group, account, state, applicant, proofMethod string
	var created time.Time
	err = tx.QueryRow(r.Context(), `SELECT tenant_id::text,group_id::text,account_id::text,state,created_at,applicant_id::text,proof_method
        FROM group_claims WHERE id=$1 AND group_id IN(SELECT id FROM native_groups WHERE platform=$2)
        FOR UPDATE`, r.PathValue("id"), requestWorkspace(r)).Scan(&tenant, &group, &account, &state, &created, &applicant, &proofMethod)
	if err == pgx.ErrNoRows {
		return fail(404, "claim_not_found")
	}
	if err != nil {
		return err
	}
	if state != "pending" {
		return fail(409, "claim_already_reviewed")
	}
	if proofMethod != "manual" {
		return fail(409, "authenticated_review_endpoint_required")
	}
	if in.Approve {
		if time.Since(created) > 24*time.Hour {
			return fail(409, "claim_expired")
		}
		if err = control.RequireTenantOwner(r.Context(), tx, control.Actor{ID: applicant, TenantID: tenant, Role: "user"}); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), `SELECT id FROM accounts WHERE id=$1 FOR SHARE`, account); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), `SELECT id FROM native_groups WHERE id=$1 FOR UPDATE`, group); err != nil {
			return err
		}
		var eligible bool
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM account_groups ag JOIN accounts a ON a.id=ag.account_id
            WHERE ag.group_id=$1 AND ag.account_id=$2 AND a.purpose='management' AND a.connection_enabled AND ag.role IN('admin','owner')
            AND ag.observation_source='native' AND ag.observed_at>now()-interval '60 seconds')`, group, account).Scan(&eligible); err != nil {
			return err
		}
		if !eligible {
			return fail(409, "fresh_native_role_required")
		}
		var existingTenant string
		var existingVersion int64
		var existingActive bool
		err = tx.QueryRow(r.Context(), `SELECT tenant_id::text,version,active FROM group_bindings WHERE group_id=$1 FOR UPDATE`, group).
			Scan(&existingTenant, &existingVersion, &existingActive)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if err == nil && (existingTenant != tenant || existingActive) {
			return fail(409, "group_already_owned")
		}
		if err = control.BindCustomerGroupTx(r.Context(), tx, tenant, requestWorkspace(r), account, group, existingVersion, ""); err != nil {
			return err
		}
		state = "approved"
	} else {
		state = "rejected"
	}
	if _, err = tx.Exec(r.Context(), `UPDATE group_claims SET state=$1,review_note=$2,reviewed_by=$3,reviewed_at=now() WHERE id=$4`,
		state, in.Note, p.ID, r.PathValue("id")); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, tenant, "claim_"+state, r.PathValue("id"), map[string]any{"group_id": group, "identity_verified": in.IdentityVerified, "note": in.Note}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]string{"state": state})
	return nil
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request, p Principal) error {
	return s.list(w, r, p, `SELECT * FROM audit_events ORDER BY id DESC LIMIT 200`)
}

func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
