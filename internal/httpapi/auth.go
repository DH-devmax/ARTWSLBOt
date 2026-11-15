package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/control"
	"wangshangliao/internal/security"
)

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if !s.validOrigin(r) {
		sendError(w, fail(403, "origin_rejected"))
		return
	}
	var in struct {
		Username      string `json:"username"`
		Password      string `json:"password"`
		CaptchaTicket string `json:"captcha_ticket"`
	}
	if err := decode(w, r, &in); err != nil {
		sendError(w, err)
		return
	}
	if len(in.Username) > 80 || len(in.Password) > 256 {
		sendError(w, fail(400, "invalid_request"))
		return
	}
	if in.CaptchaTicket == "" {
		sendError(w, fail(400, "captcha_required"))
		return
	}
	scope, err := s.loginCaptchaScope(w, r, in.Username, false)
	if err != nil {
		sendError(w, err)
		return
	}
	if err := s.captcha.Consume(scope, in.CaptchaTicket); err != nil {
		sendError(w, captchaFailure(err))
		return
	}
	now := time.Now()
	key := remoteIP(r)
	s.loginMu.Lock()
	for k, a := range s.loginAttempts {
		if now.After(a.until) {
			delete(s.loginAttempts, k)
		}
	}
	a := s.loginAttempts[key]
	if a == nil {
		if len(s.loginAttempts) >= 10000 {
			s.loginMu.Unlock()
			sendError(w, fail(429, "login_rate_limited"))
			return
		}
		a = &loginAttempt{until: now.Add(10 * time.Minute)}
		s.loginAttempts[key] = a
	}
	a.count++
	limited := a.count > 10
	s.loginMu.Unlock()
	if limited {
		w.Header().Set("Retry-After", "600")
		sendError(w, fail(429, "login_rate_limited"))
		return
	}
	select {
	case s.loginSlots <- struct{}{}:
		defer func() { <-s.loginSlots }()
	default:
		sendError(w, fail(429, "login_busy"))
		return
	}
	var p Principal
	hash := s.dummyHash
	var foundHash string
	err = s.DB.Platform.QueryRow(r.Context(), `SELECT id::text,coalesce(tenant_id::text,''),username,role,tenant_role,password_hash
        FROM users WHERE username=$1 AND NOT disabled`, in.Username).Scan(&p.ID, &p.TenantID, &p.Username, &p.Role, &p.TenantRole, &foundHash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		sendError(w, fail(503, "database_unavailable"))
		return
	}
	if err == nil {
		hash = foundHash
	}
	valid := security.VerifyPassword(hash, in.Password)
	if err != nil || !valid {
		sendError(w, fail(401, "invalid_credentials"))
		return
	}
	token, err := security.Token()
	if err != nil {
		sendError(w, err)
		return
	}
	csrf, err := security.Token()
	if err != nil {
		sendError(w, err)
		return
	}
	expires := time.Now().Add(12 * time.Hour)
	_, err = s.DB.Platform.Exec(r.Context(), `INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at)
        VALUES($1,$2,$3,$4)`, security.Digest(token), security.Digest(csrf), p.ID, expires)
	if err != nil {
		sendError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "wsl_session", Value: token, Path: "/", HttpOnly: true, Secure: s.SecureCookie,
		SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: 43200,
	})
	http.SetCookie(w, &http.Cookie{
		Name: "wsl_csrf", Value: csrf, Path: "/", Secure: s.SecureCookie,
		SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: 43200,
	})
	send(w, 200, map[string]any{"user": p, "csrf_token": csrf})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, p Principal) error {
	c, _ := r.Cookie("wsl_session")
	if _, err := s.DB.Platform.Exec(r.Context(), `DELETE FROM sessions WHERE token_hash=$1`, security.Digest(c.Value)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "wsl_session", Value: "", Path: "/", HttpOnly: true,
		Secure: s.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "wsl_csrf", Value: "", Path: "/",
		Secure: s.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	send(w, 200, map[string]bool{"logged_out": true})
	return nil
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, p Principal) error {
	permissions, err := s.permissions(r.Context(), p)
	if err != nil {
		return err
	}
	consoles, defaultConsole := availableConsoles(p)
	system, business := []string{}, []string{}
	for _, code := range permissions {
		if strings.HasPrefix(code, "group") || strings.HasPrefix(code, "assistant:") ||
			strings.HasPrefix(code, "feature:") || strings.HasPrefix(code, "operations:") ||
			strings.HasPrefix(code, "billing:read") {
			business = append(business, code)
		} else if p.Role == "superadmin" {
			system = append(system, code)
		}
	}
	send(w, 200, map[string]any{"user": p, "permissions": permissions, "consoles": consoles,
		"default_console": defaultConsole, "permission_categories": map[string]any{"system": system, "business": business}})
	return nil
}

func (s *Server) users(w http.ResponseWriter, r *http.Request, p Principal) error {
	return s.list(w, r, p, `SELECT u.id,u.tenant_id,u.username,u.role,u.tenant_role,u.disabled,u.created_at,t.name tenant_name,t.owner_id,
        sub.plan_id,sub.expires_at,sub.enabled subscription_enabled,sub.version subscription_version
        FROM users u LEFT JOIN tenants t ON t.id=u.tenant_id
        LEFT JOIN subscriptions sub ON sub.tenant_id=u.tenant_id ORDER BY u.created_at DESC LIMIT 200`)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		TenantName string `json:"tenant_name"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	in.Username = strings.TrimSpace(in.Username)
	if len(in.Username) < 3 || len(in.Username) > 80 || strings.TrimSpace(in.TenantName) == "" || len(in.TenantName) > 100 {
		return fail(400, "invalid_user")
	}
	hash, err := security.Password(in.Password)
	if err != nil {
		return fail(400, "password_too_short")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var tenantID, userID string
	if err = tx.QueryRow(r.Context(), `INSERT INTO tenants(name) VALUES($1) RETURNING id::text`, in.TenantName).Scan(&tenantID); err != nil {
		return err
	}
	if err = tx.QueryRow(r.Context(), `INSERT INTO users(tenant_id,username,password_hash,role)
        VALUES($1,$2,$3,'user') ON CONFLICT(username) DO NOTHING RETURNING id::text`, tenantID, in.Username, hash).Scan(&userID); err == pgx.ErrNoRows {
		return fail(409, "username_exists")
	} else if err != nil {
		return err
	}
	if err = control.SeedTenantRoles(r.Context(), tx, tenantID, tenantPermissions); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE tenants SET owner_id=$2 WHERE id=$1`, tenantID, userID); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE users SET tenant_role='owner' WHERE id=$1`, userID); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, tenantID, "user_created", userID, map[string]any{"username": in.Username}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 201, map[string]string{"id": userID, "tenant_id": tenantID})
	return nil
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Disabled bool `json:"disabled"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if r.PathValue("id") == p.ID {
		return fail(409, "cannot_disable_self")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var tenant, role string
	var disabled bool
	if err = tx.QueryRow(r.Context(), `SELECT tenant_id::text,tenant_role,disabled FROM users WHERE id=$1 AND role='user'`, r.PathValue("id")).Scan(&tenant, &role, &disabled); err == pgx.ErrNoRows {
		return fail(404, "user_not_found")
	} else if err != nil {
		return err
	}
	if role == "owner" && in.Disabled {
		return fail(409, "last_tenant_owner")
	}
	if _, err = tx.Exec(r.Context(), `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, tenant); err != nil {
		return err
	}
	if disabled && !in.Disabled {
		if err = control.CheckMemberSeats(r.Context(), tx, tenant); err != nil {
			return err
		}
	}
	tag, err := tx.Exec(r.Context(), `UPDATE users SET disabled=$1 WHERE id=$2 AND role='user'`, in.Disabled, r.PathValue("id"))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fail(404, "user_not_found")
	}
	if in.Disabled {
		if _, err = tx.Exec(r.Context(), `DELETE FROM sessions WHERE user_id=$1`, r.PathValue("id")); err != nil {
			return err
		}
	}
	if err = auditTx(r.Context(), tx, p, "", "user_disabled_changed", r.PathValue("id"), in); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, in)
	return nil
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request, p Principal) error {
	if p.Role == "superadmin" {
		var blob []byte
		err := s.DB.Platform.QueryRow(r.Context(), `SELECT jsonb_build_object(
            'accounts',(SELECT count(*) FROM accounts WHERE workspace=$1),
            'pool_accounts',(SELECT count(*) FROM accounts WHERE purpose=CASE WHEN $1='maibot' THEN 'chat' ELSE 'management' END),
            'pool_groups',(SELECT count(*) FROM group_bindings WHERE active),
            'groups',(SELECT count(*) FROM group_bindings b JOIN native_groups g ON g.id=b.group_id WHERE b.active AND
                (g.platform=$1 OR $1='maibot' AND (b.ai_account_id IS NOT NULL OR b.ai_requested))),
            'ai_groups',(SELECT count(*) FROM group_bindings b JOIN native_groups g ON g.id=b.group_id WHERE b.active AND b.ai_enabled
                AND (g.platform=$1 OR $1='maibot')),
            'ai_requests',(SELECT count(*) FROM group_bindings b JOIN native_groups g ON g.id=b.group_id WHERE b.active AND b.ai_requested
                AND (g.platform=$1 OR $1='maibot')),
            'pending_claims',(SELECT count(*) FROM group_claims c JOIN native_groups g ON g.id=c.group_id WHERE c.state='pending' AND g.platform=$1),
            'queued_operations',(SELECT count(*) FROM operations o JOIN native_groups g ON g.id=o.group_id WHERE o.status='queued' AND g.platform=$1),
            'unknown_operations',(SELECT count(*) FROM operations o JOIN native_groups g ON g.id=o.group_id WHERE o.status='unknown' AND g.platform=$1),
            'native_ready',false,'ai_ready',false)`, requestWorkspace(r)).Scan(&blob)
		if err != nil {
			return err
		}
		performance := s.workspaceDecodeDistribution(requestWorkspace(r))
		sendOverview(w, blob, &performance)
		return nil
	}
	var blob []byte
	err := s.DB.TenantTx(r.Context(), p.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT jsonb_build_object(
            'accounts',(SELECT count(DISTINCT primary_account_id) FROM group_bindings b JOIN native_groups g ON g.id=b.group_id WHERE g.platform=$1),
            'groups',(SELECT count(*) FROM group_bindings b JOIN native_groups g ON g.id=b.group_id WHERE b.active AND g.platform=$1),
            'ai_groups',(SELECT count(*) FROM group_bindings b JOIN native_groups g ON g.id=b.group_id WHERE b.active AND b.ai_enabled AND g.platform=$1),
            'pending_claims',(SELECT count(*) FROM group_claims c JOIN native_groups g ON g.id=c.group_id WHERE c.state='pending' AND g.platform=$1),
            'queued_operations',(SELECT count(*) FROM operations o JOIN native_groups g ON g.id=o.group_id WHERE o.status='queued' AND g.platform=$1),
            'unknown_operations',(SELECT count(*) FROM operations o JOIN native_groups g ON g.id=o.group_id WHERE o.status='unknown' AND g.platform=$1),
            'native_ready',false,'ai_ready',false)`, requestWorkspace(r)).Scan(&blob)
	})
	if err != nil {
		return err
	}
	sendOverview(w, blob)
	return nil
}

func sendOverview(w http.ResponseWriter, blob []byte, performance ...*latencyDistribution) {
	var result map[string]any
	if err := json.Unmarshal(blob, &result); err != nil {
		sendError(w, fail(500, "internal_error"))
		return
	}
	result["limits"] = workspaceLimits()
	if len(performance) > 0 && performance[0] != nil {
		result["decode_latency"] = performance[0]
	}
	send(w, 200, result)
}
