package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/control"
	"wangshangliao/internal/security"
)

var supportedModules = map[string]bool{
	"moderation": true, "replies": true, "schedules": true, "rankings": true,
	"cards": true, "friends": true, "activities": true, "finance": true, "ai": true,
}

func (s *Server) accounts(w http.ResponseWriter, r *http.Request, p Principal) error {
	columns := `id,name,workspace,native_uid,connection_enabled,testing_enabled,
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
		Name string `json:"name"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 {
		return fail(400, "invalid_name")
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
	if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM accounts WHERE workspace=$1`, requestWorkspace(r)).Scan(&count); err != nil {
		return err
	}
	if count >= control.MaxAccountsPerWorkspace {
		return fail(409, "account_limit_reached")
	}
	var id string
	if err = tx.QueryRow(r.Context(), `INSERT INTO accounts(name,workspace) VALUES($1,$2) RETURNING id::text`,
		strings.TrimSpace(in.Name), requestWorkspace(r)).Scan(&id); err != nil {
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
	if in.ConnectionEnabled != nil && *in.ConnectionEnabled {
		return fail(409, "native_compatibility_not_verified")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
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
	send(w, 200, map[string]int64{"version": version})
	return nil
}

func (s *Server) plans(w http.ResponseWriter, r *http.Request, p Principal) error {
	return s.list(w, r, p, `SELECT * FROM plans ORDER BY name LIMIT 100`)
}

func (s *Server) createPlan(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Name            string   `json:"name"`
		GroupLimit      int      `json:"group_limit"`
		AIGroupLimit    int      `json:"ai_group_limit"`
		MonthlyAITokens int64    `json:"monthly_ai_tokens"`
		Modules         []string `json:"modules"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 || in.GroupLimit < 0 || in.GroupLimit > control.MaxAuthorizedGroups ||
		in.AIGroupLimit < 0 || in.AIGroupLimit > control.MaxAuthorizedGroups || in.AIGroupLimit > in.GroupLimit || in.MonthlyAITokens < 0 {
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
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var id string
	if err = tx.QueryRow(r.Context(), `INSERT INTO plans(name,group_limit,ai_group_limit,monthly_ai_tokens,modules)
        VALUES($1,$2,$3,$4,$5) RETURNING id::text`, in.Name, in.GroupLimit, in.AIGroupLimit, in.MonthlyAITokens, in.Modules).Scan(&id); err != nil {
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
	if !validUUID(in.PlanID) || !in.ExpiresAt.After(time.Now()) || in.Version < 0 {
		return fail(400, "invalid_subscription")
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
            g.name,g.native_id,a.name account_name,b.version FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
            JOIN accounts a ON a.id=b.ai_account_id AND a.workspace='maibot' ORDER BY g.name LIMIT 100`)
	}
	return s.list(w, r, p, `SELECT b.*,g.name,g.native_id,a.name account_name
        FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
        JOIN accounts a ON a.id=b.primary_account_id ORDER BY g.name LIMIT 100`)
}

// GroupConfig contains no management chat commands or unbounded regular expressions.
type GroupConfig struct {
	BlockedWords   []string `json:"blocked_words"`
	Blacklist      []string `json:"blacklist"`
	Exemptions     []string `json:"exemptions"`
	KeywordReplies []struct {
		Keyword string `json:"keyword"`
		Reply   string `json:"reply"`
	} `json:"keyword_replies"`
	AICooldownSeconds int `json:"ai_cooldown_seconds"`
}

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
	err = tx.QueryRow(r.Context(), `SELECT tenant_id::text,config,version FROM group_bindings
        WHERE group_id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&tenant, &stored, &currentVersion)
	if err == pgx.ErrNoRows || (err == nil && p.Role != "superadmin" && tenant != p.TenantID) {
		return fail(404, "group_not_found")
	}
	if err != nil {
		return err
	}
	if in.Version != currentVersion {
		return fail(409, "version_conflict")
	}
	var previous GroupConfig
	if err = json.Unmarshal(stored, &previous); err != nil {
		return err
	}
	var modules []string
	err = tx.QueryRow(r.Context(), `SELECT p.modules FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
        WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>now()`, tenant).Scan(&modules)
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
	permissions, err := s.permissions(r.Context(), p)
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
	blob, _ := json.Marshal(in.Config)
	var version int64
	err = tx.QueryRow(r.Context(), `UPDATE group_bindings SET management_enabled=$1,ai_enabled=$2,
        config=$3,version=version+1,epoch=epoch+1 WHERE group_id=$4 AND version=$5 RETURNING version`,
		in.ManagementEnabled, in.AIEnabled, blob, r.PathValue("id"), in.Version).Scan(&version)
	if err == pgx.ErrNoRows {
		return fail(409, "version_conflict")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE operations SET status='cancelled',result_code='group_changed',updated_at=now()
        WHERE group_id=$1 AND status='queued'`, r.PathValue("id")); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, tenant, "group_updated", r.PathValue("id"), map[string]any{"version": version, "management_enabled": in.ManagementEnabled, "ai_enabled": in.AIEnabled}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]int64{"version": version})
	return nil
}

func (s *Server) personas(w http.ResponseWriter, r *http.Request, p Principal) error {
	return s.list(w, r, p, `SELECT p.*,a.name account_name FROM personas p JOIN accounts a ON a.id=p.account_id
        WHERE a.workspace='maibot' ORDER BY a.name LIMIT 10`)
}

func (s *Server) savePersona(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Nickname    string `json:"nickname"`
		Personality string `json:"personality"`
		ReplyStyle  string `json:"reply_style"`
		Version     int64  `json:"version"`
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
		err = tx.QueryRow(r.Context(), `INSERT INTO personas(account_id,nickname,personality,reply_style) VALUES($1,$2,$3,$4)
            ON CONFLICT DO NOTHING RETURNING version`, r.PathValue("id"), in.Nickname, in.Personality, in.ReplyStyle).Scan(&version)
	} else {
		err = tx.QueryRow(r.Context(), `UPDATE personas SET nickname=$1,personality=$2,reply_style=$3,version=version+1
            WHERE account_id=$4 AND version=$5 RETURNING version`, in.Nickname, in.Personality, in.ReplyStyle, r.PathValue("id"), in.Version).Scan(&version)
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
	return s.list(w, r, p, `SELECT c.*,g.name group_name,g.native_id FROM group_claims c JOIN native_groups g ON g.id=c.group_id
        ORDER BY c.created_at DESC LIMIT 200`)
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
	var group, account string
	err = tx.QueryRow(r.Context(), `UPDATE claim_codes c SET consumed_at=now() FROM accounts a
        WHERE c.account_id=a.id AND a.workspace='wsl' AND a.connection_enabled AND a.epoch=c.account_epoch
        AND c.code_hash=$1 AND c.consumed_at IS NULL AND c.expires_at>now()
        RETURNING c.group_id::text,c.account_id::text`, security.Digest(in.Code)).Scan(&group, &account)
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
	var tenant, group, account, state string
	var created time.Time
	err = tx.QueryRow(r.Context(), `SELECT tenant_id::text,group_id::text,account_id::text,state,created_at
        FROM group_claims WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&tenant, &group, &account, &state, &created)
	if err == pgx.ErrNoRows {
		return fail(404, "claim_not_found")
	}
	if err != nil {
		return err
	}
	if state != "pending" {
		return fail(409, "claim_already_reviewed")
	}
	if in.Approve {
		if time.Since(created) > 24*time.Hour {
			return fail(409, "claim_expired")
		}
		var limit, count, total int
		err = tx.QueryRow(r.Context(), `SELECT p.group_limit FROM subscriptions s JOIN plans p ON p.id=s.plan_id
            WHERE s.tenant_id=$1 AND s.enabled AND s.expires_at>now() FOR UPDATE OF s`, tenant).Scan(&limit)
		if err == pgx.ErrNoRows {
			return fail(409, "active_subscription_required")
		}
		if err != nil {
			return err
		}
		if err = tx.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE tenant_id=$1),count(*) FROM group_bindings WHERE active`, tenant).Scan(&count, &total); err != nil {
			return err
		}
		if count >= limit || total >= control.MaxAuthorizedGroups {
			return fail(409, "group_quota_exceeded")
		}
		if _, err = tx.Exec(r.Context(), `SELECT id FROM accounts WHERE id=$1 FOR SHARE`, account); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), `SELECT id FROM native_groups WHERE id=$1 FOR UPDATE`, group); err != nil {
			return err
		}
		var eligible bool
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM account_groups ag JOIN accounts a ON a.id=ag.account_id
            WHERE ag.group_id=$1 AND ag.account_id=$2 AND a.workspace='wsl' AND a.connection_enabled AND ag.role IN('admin','owner')
            AND ag.observation_source='native' AND ag.observed_at>now()-interval '60 seconds')`, group, account).Scan(&eligible); err != nil {
			return err
		}
		if !eligible {
			return fail(409, "fresh_native_role_required")
		}
		tag, err := tx.Exec(r.Context(), `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id)
            VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, group, tenant, account)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fail(409, "group_already_owned")
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
