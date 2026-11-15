package control

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wangshangliao/internal/security"
)

var (
	ErrFreshRole = errors.New("fresh_native_role_required")
	ErrLiveCode  = errors.New("claim_code_already_issued")
	ErrOwned     = errors.New("group_already_owned")
)

// ApplyNativeRole is an internal transport boundary, not a user-supplied role API.
// Replay adapters must never call it or mark observations as native.
func ApplyNativeRole(ctx context.Context, db *pgxpool.Pool, account, group, role string, observed time.Time) (bool, error) {
	if !slices.Contains([]string{"unknown", "member", "admin", "owner", "left"}, role) || observed.IsZero() || observed.After(time.Now().Add(time.Second)) {
		return false, ErrFreshRole
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	changed, err := applyNativeRoleTx(ctx, tx, account, group, role, observed)
	if err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return changed, nil
}

func applyNativeRoleTx(ctx context.Context, tx pgx.Tx, account, group, role string, observed time.Time) (bool, error) {
	var err error
	if _, err = tx.Exec(ctx, `SELECT id FROM accounts WHERE id=$1 FOR SHARE`, account); err != nil {
		return false, err
	}
	var groupID string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM native_groups WHERE id=$1 FOR UPDATE`, group).Scan(&groupID); err != nil {
		return false, err
	}
	var oldRole, oldSource string
	var oldTime time.Time
	err = tx.QueryRow(ctx, `SELECT role,observed_at,observation_source FROM account_groups WHERE account_id=$1 AND group_id=$2 FOR UPDATE`, account, group).Scan(&oldRole, &oldTime, &oldSource)
	if err != nil && err != pgx.ErrNoRows {
		return false, err
	}
	if err == nil && !observed.After(oldTime) {
		return false, nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
        VALUES($1,$2,$3,$4,'native') ON CONFLICT(account_id,group_id) DO UPDATE SET
        role=excluded.role,observed_at=excluded.observed_at,observation_source='native'`, account, group, role, observed)
	if err != nil {
		return false, err
	}
	changed := oldRole != role || oldSource != "native"
	if changed {
		_, err = tx.Exec(ctx, `UPDATE group_bindings SET version=version+1,epoch=epoch+1,
            management_enabled=CASE WHEN primary_account_id=$2 AND $3 NOT IN('admin','owner') THEN false ELSE management_enabled END,
            ai_enabled=CASE WHEN (primary_account_id=$2 AND $3 NOT IN('admin','owner'))
                OR (ai_account_id=$2 AND $3 IN('left','unknown')) THEN false ELSE ai_enabled END
            WHERE group_id=$1 AND (primary_account_id=$2 OR ai_account_id=$2)`, group, account, role)
		if err != nil {
			return false, err
		}
		_, err = tx.Exec(ctx, `UPDATE operations SET status='cancelled',result_code='native_role_changed',updated_at=now()
            WHERE group_id=$1 AND status='queued'`, group)
		if err != nil {
			return false, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,action,resource_id,detail)
            SELECT tenant_id,'native_role_changed',group_id::text,jsonb_build_object('account_id',$2::text,'role',$3::text)
            FROM group_bindings WHERE group_id=$1::uuid`, group, account, role)
		if err != nil {
			return false, err
		}
	}
	return changed, nil
}

// IssueClaimCode returns the secret exactly once; only its hash is stored.
// Delivery still requires the compatible sender and account/binding revalidation.
func IssueClaimCode(ctx context.Context, db *pgxpool.Pool, account, group string) (string, time.Time, error) {
	return issueClaimCode(ctx, db, nil, account, group)
}

// IssueClaimCodeForSession is the administrative boundary. Codes never appear
// in audit details and do not themselves grant tenant ownership.
func IssueClaimCodeForSession(ctx context.Context, db *pgxpool.Pool, ref SessionRef, account, group string) (string, time.Time, error) {
	return issueClaimCode(ctx, db, &ref, account, group)
}

func issueClaimCode(ctx context.Context, db *pgxpool.Pool, ref *SessionRef, account, group string) (string, time.Time, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return "", time.Time{}, err
	}
	var actor Actor
	if ref != nil {
		actor, err = sessionActor(ctx, tx, *ref, true)
		if err != nil {
			return "", time.Time{}, err
		}
		if actor.Role != "superadmin" {
			return "", time.Time{}, Denied(403, "superadmin_required")
		}
		var scoped bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts a JOIN native_groups g ON g.id=$2
            AND g.platform=a.platform WHERE a.id=$1 AND a.platform IN('wsl','lajiaoliao')
            AND a.workspace=a.platform AND a.purpose='management' AND a.auth_state='authenticated'
            AND a.credential_sealed IS NOT NULL)`, account, group).Scan(&scoped); err != nil {
			return "", time.Time{}, err
		}
		if !scoped {
			return "", time.Time{}, ErrFreshRole
		}
	}
	var epoch int64
	err = tx.QueryRow(ctx, `SELECT epoch FROM accounts WHERE id=$1 AND purpose='management' AND connection_enabled
        AND (NOT $2 OR workspace=platform AND platform IN('wsl','lajiaoliao')
            AND auth_state='authenticated' AND credential_sealed IS NOT NULL)
        FOR SHARE`, account, ref != nil).Scan(&epoch)
	if err == pgx.ErrNoRows {
		return "", time.Time{}, ErrFreshRole
	}
	if err != nil {
		return "", time.Time{}, err
	}
	var groupID string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM native_groups WHERE id=$1 FOR UPDATE`, group).Scan(&groupID); err != nil {
		return "", time.Time{}, err
	}
	var eligible, owned, live bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account_groups WHERE account_id=$1 AND group_id=$2
        AND role IN('admin','owner') AND observation_source='native' AND observed_at>now()-interval '60 seconds'),
        EXISTS(SELECT 1 FROM group_bindings WHERE group_id=$2),
        EXISTS(SELECT 1 FROM claim_codes WHERE group_id=$2 AND consumed_at IS NULL AND expires_at>now())`,
		account, group).Scan(&eligible, &owned, &live)
	if err != nil {
		return "", time.Time{}, err
	}
	if !eligible {
		return "", time.Time{}, ErrFreshRole
	}
	targetTenant := ""
	if owned {
		if ref == nil {
			return "", time.Time{}, ErrOwned
		}
		var active bool
		if err = tx.QueryRow(ctx, `SELECT tenant_id::text,active FROM group_bindings WHERE group_id=$1 FOR UPDATE`, group).
			Scan(&targetTenant, &active); err != nil {
			return "", time.Time{}, err
		}
		if active {
			return "", time.Time{}, ErrOwned
		}
	}
	if live {
		return "", time.Time{}, ErrLiveCode
	}
	if _, err = tx.Exec(ctx, `UPDATE claim_codes SET consumed_at=now() WHERE group_id=$1 AND consumed_at IS NULL`, group); err != nil {
		return "", time.Time{}, err
	}
	token, err := security.Token()
	if err != nil {
		return "", time.Time{}, err
	}
	var expires time.Time
	err = tx.QueryRow(ctx, `INSERT INTO claim_codes(code_hash,group_id,account_id,account_epoch,expires_at,target_tenant_id)
        VALUES($1,$2,$3,$4,now()+interval '10 minutes',nullif($5,'')::uuid) RETURNING expires_at`,
		security.Digest(token), group, account, epoch, targetTenant).Scan(&expires)
	if err != nil {
		return "", time.Time{}, err
	}
	if ref != nil {
		if _, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,resource_id,detail)
            VALUES($1,'claim_code_issued',$2,jsonb_build_object('account_id',$3::text,'account_epoch',$4::bigint))`,
			actor.ID, group, account, epoch); err != nil {
			return "", time.Time{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}
