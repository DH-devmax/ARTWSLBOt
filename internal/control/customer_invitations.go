package control

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/database"
	"wangshangliao/internal/security"
)

type CustomerInvitationService struct {
	DB      *database.DB
	Catalog []PermissionSpec
}

type CustomerInvitationCreate struct {
	Mode           string `json:"mode"`
	TenantName     string `json:"tenant_name"`
	ExistingUserID string `json:"existing_user_id,omitempty"`
	Key            string `json:"-"`
}

type CustomerInvitation struct {
	ID        string    `json:"id"`
	Mode      string    `json:"mode"`
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expires_at"`
	Token     string    `json:"token,omitempty"`
}

type CustomerInvitationAcceptance struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	State    string `json:"state"`
}

func (s *CustomerInvitationService) Create(ctx context.Context, ref SessionRef, in CustomerInvitationCreate) (CustomerInvitation, error) {
	var out CustomerInvitation
	in.TenantName = strings.TrimSpace(in.TenantName)
	if !onboardingKey(in.Key) || in.Mode != "new" && in.Mode != "existing" ||
		in.Mode == "new" && (in.ExistingUserID != "" || !onboardingLabel(in.TenantName, 100)) ||
		in.Mode == "existing" && (!ValidID(in.ExistingUserID) || in.TenantName != "") {
		return out, Denied(400, "customer_invitation_invalid")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	a, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return out, err
	}
	if a.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798497)`); err != nil {
		return out, err
	}
	raw, _ := json.Marshal(in)
	digest := security.Digest(string(raw))
	var previous []byte
	err = tx.QueryRow(ctx, `SELECT id::text,mode,state,expires_at,request_hash FROM customer_invitations
		WHERE created_by=$1 AND create_key=$2`, a.ID, in.Key).Scan(&out.ID, &out.Mode, &out.State, &out.ExpiresAt, &previous)
	if err == nil {
		if !bytes.Equal(previous, digest) {
			return CustomerInvitation{}, Denied(409, "idempotency_conflict")
		}
		return out, nil
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	if in.Mode == "existing" {
		var id string
		err = tx.QueryRow(ctx, `SELECT u.id::text FROM users u JOIN tenants t ON t.id=u.tenant_id
			WHERE u.id=$1 AND u.role='user' AND u.tenant_role='owner' AND t.owner_id=u.id
			AND NOT u.disabled FOR SHARE OF u,t`, in.ExistingUserID).Scan(&id)
		if err == pgx.ErrNoRows {
			return out, Denied(409, "customer_invitation_owner_required")
		}
		if err != nil {
			return out, err
		}
	}
	out.ID, out.Mode, out.State = NewID(), in.Mode, "pending"
	out.Token, err = security.Token()
	if err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO customer_invitations
		(id,token_hash,created_by,mode,tenant_name,existing_user_id,create_key,request_hash)
		VALUES($1,$2,$3,$4,$5,nullif($6,'')::uuid,$7,$8) RETURNING expires_at`,
		out.ID, security.Digest(out.Token), a.ID, in.Mode, in.TenantName, in.ExistingUserID, in.Key, digest).Scan(&out.ExpiresAt)
	if err != nil {
		return CustomerInvitation{}, err
	}
	if err = onboardingAudit(ctx, tx, a, "", "customer_invitation_created", out.ID, map[string]any{"mode": in.Mode}); err != nil {
		return CustomerInvitation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CustomerInvitation{}, err
	}
	return out, nil
}

// Preview takes the secret in a POST body so it is not logged in a URL. It never
// reveals the target's username, tenant, password state or native identity.
func (s *CustomerInvitationService) Preview(ctx context.Context, token string) (CustomerInvitation, error) {
	var out CustomerInvitation
	if !onboardingToken(token) {
		return out, Denied(400, "customer_invitation_invalid_or_expired")
	}
	err := s.DB.Platform.QueryRow(ctx, `SELECT i.id::text,i.mode,
		CASE WHEN i.state='pending' AND i.expires_at<=clock_timestamp() THEN 'expired' ELSE i.state END,i.expires_at
		FROM customer_invitations i JOIN users u ON u.id=i.created_by
		WHERE i.token_hash=$1 AND NOT u.disabled AND u.role='superadmin'`,
		security.Digest(token)).Scan(&out.ID, &out.Mode, &out.State, &out.ExpiresAt)
	if err == pgx.ErrNoRows {
		return out, Denied(400, "customer_invitation_invalid_or_expired")
	}
	return out, err
}

// ActivateNew never logs the user in and never changes an existing password.
// Consumption, tenant creation and owner assignment commit in one transaction.
func (s *CustomerInvitationService) ActivateNew(ctx context.Context, token, username, password string) (CustomerInvitationAcceptance, error) {
	var out CustomerInvitationAcceptance
	username = strings.TrimSpace(username)
	if !onboardingToken(token) || !onboardingLabel(username, 80) || len(username) < 3 {
		return out, Denied(400, "customer_invitation_activation_invalid")
	}
	preview, err := s.Preview(ctx, token)
	if err != nil {
		return out, err
	}
	if preview.Mode != "new" {
		return out, Denied(401, "customer_invitation_website_login_required")
	}
	if preview.State != "pending" {
		return out, Denied(409, "customer_invitation_not_pending")
	}
	hash, err := security.Password(password)
	if err != nil {
		return out, Denied(400, "customer_invitation_password_invalid")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	var id, tenantName string
	err = tx.QueryRow(ctx, `SELECT i.id::text,i.tenant_name FROM customer_invitations i JOIN users u ON u.id=i.created_by
		WHERE i.token_hash=$1 AND i.mode='new' AND i.state='pending' AND i.expires_at>clock_timestamp()
		AND u.role='superadmin' AND NOT u.disabled FOR UPDATE OF i FOR SHARE OF u`, security.Digest(token)).Scan(&id, &tenantName)
	if err == pgx.ErrNoRows {
		return out, Denied(409, "customer_invitation_not_pending")
	}
	if err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO tenants(name) VALUES($1) RETURNING id::text`, tenantName).Scan(&out.TenantID)
	if err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO users(tenant_id,username,password_hash,role,tenant_role)
		VALUES($1,$2,$3,'user','owner') ON CONFLICT(username) DO NOTHING RETURNING id::text`,
		out.TenantID, username, hash).Scan(&out.UserID)
	if err == pgx.ErrNoRows {
		return CustomerInvitationAcceptance{}, Denied(409, "customer_invitation_username_unavailable")
	}
	if err != nil {
		return out, err
	}
	if err = SeedTenantRoles(ctx, tx, out.TenantID, s.Catalog); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `UPDATE tenants SET owner_id=$2 WHERE id=$1`, out.TenantID, out.UserID); err != nil {
		return out, err
	}
	if err = acceptCustomerInvitation(ctx, tx, id, out); err != nil {
		return out, err
	}
	a := Actor{ID: out.UserID, TenantID: out.TenantID, Role: "user"}
	if err = onboardingAudit(ctx, tx, a, out.TenantID, "customer_invitation_accepted", id, map[string]any{"mode": "new"}); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CustomerInvitationAcceptance{}, err
	}
	out.State = "accepted"
	return out, nil
}

func (s *CustomerInvitationService) AcceptExisting(ctx context.Context, ref SessionRef, token string) (CustomerInvitationAcceptance, error) {
	var out CustomerInvitationAcceptance
	if !onboardingToken(token) {
		return out, Denied(400, "customer_invitation_invalid_or_expired")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	a, err := onboardingOwner(ctx, tx, ref, false)
	if err != nil {
		return out, err
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT i.id::text FROM customer_invitations i JOIN users u ON u.id=i.created_by
		WHERE i.token_hash=$1 AND i.mode='existing' AND i.existing_user_id=$2 AND i.state='pending'
		AND i.expires_at>clock_timestamp() AND u.role='superadmin' AND NOT u.disabled
		FOR UPDATE OF i FOR SHARE OF u`, security.Digest(token), a.ID).Scan(&id)
	if err == pgx.ErrNoRows {
		return out, Denied(409, "customer_invitation_not_pending")
	}
	if err != nil {
		return out, err
	}
	out.UserID, out.TenantID, out.State = a.ID, a.TenantID, "accepted"
	if err = acceptCustomerInvitation(ctx, tx, id, out); err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, a, a.TenantID, "customer_invitation_accepted", id, map[string]any{"mode": "existing"}); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func acceptCustomerInvitation(ctx context.Context, tx pgx.Tx, id string, a CustomerInvitationAcceptance) error {
	tag, err := tx.Exec(ctx, `UPDATE customer_invitations SET state='accepted',accepted_user_id=$2,
		accepted_tenant_id=$3,accepted_at=clock_timestamp() WHERE id=$1 AND state='pending' AND expires_at>clock_timestamp()`,
		id, a.UserID, a.TenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return Denied(409, "customer_invitation_not_pending")
	}
	return nil
}

func (s *CustomerInvitationService) Revoke(ctx context.Context, ref SessionRef, id string) error {
	if !ValidID(id) {
		return Denied(400, "customer_invitation_invalid")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	a, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return err
	}
	if a.Role != "superadmin" {
		return Denied(403, "superadmin_required")
	}
	tag, err := tx.Exec(ctx, `UPDATE customer_invitations SET state='revoked' WHERE id=$1 AND state='pending'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return Denied(409, "customer_invitation_not_pending")
	}
	if err = onboardingAudit(ctx, tx, a, "", "customer_invitation_revoked", id, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func onboardingOwner(ctx context.Context, q Queryer, ref SessionRef, lock bool) (Actor, error) {
	a, err := sessionActor(ctx, q, ref, lock)
	if err != nil {
		return a, err
	}
	if a.Role != "user" || !ValidID(a.TenantID) {
		return a, Denied(403, "tenant_owner_required")
	}
	sql := `SELECT u.id::text FROM users u JOIN tenants t ON t.id=u.tenant_id
		WHERE u.id=$1 AND u.tenant_id=$2 AND u.tenant_role='owner' AND t.owner_id=u.id AND NOT u.disabled`
	if lock {
		sql += ` FOR SHARE OF u,t`
	}
	var id string
	if err = q.QueryRow(ctx, sql, a.ID, a.TenantID).Scan(&id); err == pgx.ErrNoRows {
		return a, Denied(403, "tenant_owner_required")
	}
	return a, err
}

func onboardingAudit(ctx context.Context, tx pgx.Tx, a Actor, tenant, action, id string, metadata map[string]any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
		VALUES(nullif($1,'')::uuid,$2,$3,$4,$5)`, tenant, a.ID, action, id, raw)
	return err
}

func onboardingKey(key string) bool {
	return len(key) >= 8 && len(key) <= 100 && utf8.ValidString(key) && strings.IndexFunc(key, func(r rune) bool { return r < 33 || r == 127 }) < 0
}

func onboardingToken(token string) bool {
	if len(token) != 43 {
		return false
	}
	for _, r := range token {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func onboardingLabel(label string, maxBytes int) bool {
	return len(label) > 0 && len(label) <= maxBytes && utf8.ValidString(label) &&
		strings.TrimSpace(label) == label && strings.IndexFunc(label, func(r rune) bool { return r < 32 || r == 127 }) < 0
}
