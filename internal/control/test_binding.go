package control

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TestBindingRequest struct {
	Account, Group, Tenant, Platform  string
	Version                           int64
	ConfirmIsolated, IdentityVerified bool
}

// AuthorizeTestBinding materializes an explicitly delegated isolated scope.
// Native membership never selects the tenant or confirms its website owner.
func AuthorizeTestBinding(ctx context.Context, db *pgxpool.Pool, ref SessionRef, in TestBindingRequest) error {
	if !ValidID(in.Account) || !ValidID(in.Group) || !ValidID(in.Tenant) || in.Version < 1 ||
		(in.Platform != "wsl" && in.Platform != "lajiaoliao") || !in.ConfirmIsolated || !in.IdentityVerified {
		return Denied(400, "isolated_test_identity_confirmation_required")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	actor, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return err
	}
	if actor.Role != "superadmin" {
		return Denied(403, "superadmin_required")
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return err
	}
	if err = RequireTestConnectionTx(ctx, tx, ref, in.Account, in.Version); err != nil {
		return err
	}
	var admitted bool
	if err = tx.QueryRow(ctx, `SELECT platform=$2 AND workspace=$2 AND connection_enabled
        FROM accounts WHERE id=$1`, in.Account, in.Platform).Scan(&admitted); err != nil {
		return err
	}
	if !admitted {
		return Denied(409, "native_test_admission_required")
	}
	var owner string
	err = tx.QueryRow(ctx, `SELECT u.id::text FROM tenants t JOIN users u ON u.id=t.owner_id
        WHERE t.id=$1 AND u.tenant_id=t.id AND u.tenant_role='owner' AND u.role='user'
        AND NOT u.disabled FOR SHARE OF t,u`, in.Tenant).Scan(&owner)
	if err == pgx.ErrNoRows {
		return Denied(409, "tenant_owner_confirmation_required")
	}
	if err != nil {
		return err
	}
	var group string
	err = tx.QueryRow(ctx, `SELECT id::text FROM native_groups WHERE id=$1 AND platform=$2 FOR UPDATE`,
		in.Group, in.Platform).Scan(&group)
	if err == pgx.ErrNoRows {
		return Denied(404, "group_not_found")
	}
	if err != nil {
		return err
	}
	var fresh, bound, pending bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account_groups
        WHERE account_id=$1 AND group_id=$2 AND role IN('owner','admin') AND observation_source='native'
        AND observed_at>clock_timestamp()-interval '60 seconds' AND observed_at<=clock_timestamp()),
        EXISTS(SELECT 1 FROM group_bindings WHERE group_id=$2),
        EXISTS(SELECT 1 FROM group_claims WHERE group_id=$2 AND state='pending')
        OR EXISTS(SELECT 1 FROM claim_codes WHERE group_id=$2 AND consumed_at IS NULL AND expires_at>clock_timestamp())`,
		in.Account, in.Group).Scan(&fresh, &bound, &pending)
	if err != nil {
		return err
	}
	if !fresh {
		return Denied(409, "fresh_native_role_required")
	}
	if bound {
		return Denied(409, "group_already_owned")
	}
	if pending {
		return Denied(409, "group_claim_review_required")
	}
	if err = CheckAccountCapacity(ctx, tx, in.Account, in.Group); err != nil {
		return err
	}
	var total int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM group_bindings WHERE active`).Scan(&total); err != nil {
		return err
	}
	if total >= MaxAuthorizedGroups {
		return Denied(409, "group_quota_exceeded")
	}
	if _, err = tx.Exec(ctx, `UPDATE native_groups SET isolated_test=true WHERE id=$1`, in.Group); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,management_enabled,ai_enabled)
        VALUES($1,$2,$3,false,false)`, in.Group, in.Tenant, in.Account); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,tenant_id,action,resource_id,detail)
        VALUES($1,$2,'isolated_test_binding_authorized',$3,jsonb_build_object(
        'account_id',$4::text,'account_version',$5::bigint,'platform',$6::text,
        'website_owner_id',$7::text,'identity_verified',true,'admission_mode','native_debug',
        'management_enabled',false,'ai_enabled',false))`,
		actor.ID, in.Tenant, in.Group, in.Account, in.Version, in.Platform, owner); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
