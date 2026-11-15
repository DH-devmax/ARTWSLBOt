package control

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Kept in sealed task snapshots so replacing or versioning a scope cannot
// revive work admitted by an earlier scope, even before lifecycle cleanup.
type controlledExecutionFence struct {
	ControlledScopeID      string `json:"controlled_scope_id,omitempty"`
	ControlledScopeVersion int64  `json:"controlled_scope_version,omitempty"`
	ProofWindowID          string `json:"onboarding_proof_window_id,omitempty"`
	ProofChallengeID       string `json:"onboarding_proof_challenge_id,omitempty"`
	ProofChallengeVersion  int64  `json:"onboarding_proof_challenge_version,omitempty"`
	ProofKind              string `json:"onboarding_proof_kind,omitempty"`
	ProofTextDigest        string `json:"onboarding_proof_text_digest,omitempty"`
	ProofTarget            string `json:"onboarding_proof_target,omitempty"`
	ProofAccountID         string `json:"onboarding_proof_account_id,omitempty"`
}

func controlledExecutionScope(ctx context.Context, q Queryer, actor, platform, group string) (controlledExecutionFence, error) {
	var out controlledExecutionFence
	if group == "" {
		return out, nil
	}
	var kind string
	err := q.QueryRow(ctx, `SELECT proof_kind FROM group_binding_ownership
		WHERE group_id=$1 AND platform=$2`, group, platform).Scan(&kind)
	if err == pgx.ErrNoRows || err == nil && kind != "controlled_test" {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err := RequireControlledBinding(ctx, q, actor, platform, group); err != nil {
		return out, err
	}
	err = q.QueryRow(ctx, `SELECT s.id::text,s.version FROM group_binding_ownership o
		JOIN onboarding_test_scopes s ON s.id=o.controlled_scope_id
		JOIN group_bindings b ON b.group_id=o.group_id AND b.tenant_id=o.tenant_id
		JOIN native_groups g ON g.id=s.group_id AND g.platform=s.platform
		JOIN accounts a ON a.id=s.account_id JOIN accounts c ON c.id=s.claimant_account_id
		JOIN users u ON u.id=s.actor_id JOIN tenants t ON t.id=s.tenant_id
		WHERE o.group_id=$1 AND o.platform=$2 AND o.proof_kind='controlled_test'
		AND o.state='active' AND b.active AND b.primary_account_id=s.account_id
		AND s.group_id=b.group_id AND s.tenant_id=b.tenant_id AND s.platform=o.platform
		FOR SHARE OF o,s,b,g,a,c,u,t`, group, platform).Scan(&out.ControlledScopeID, &out.ControlledScopeVersion)
	if err == pgx.ErrNoRows {
		return out, Denied(409, "controlled_onboarding_scope_inactive")
	}
	if err != nil {
		return out, err
	}
	if err = RequireControlledBinding(ctx, q, actor, platform, group); err != nil {
		return out, err
	}
	return out, nil
}

func (f controlledExecutionFence) require(ctx context.Context, q Queryer, actor, platform, group string) error {
	if f.ProofWindowID != "" {
		current, scope, found, err := controlledProofExecution(ctx, q, f.ProofWindowID, true)
		if err != nil {
			return err
		}
		if !found || current != f || actor != scope.Actor || platform != scope.Platform || group != scope.Group {
			return Denied(409, "onboarding_controlled_proof_window_invalid")
		}
		return nil
	}
	current, err := controlledExecutionScope(ctx, q, actor, platform, group)
	if err != nil {
		return err
	}
	if current != f {
		return Denied(409, "controlled_onboarding_scope_changed")
	}
	return nil
}

// The caller first locks and validates the binding and its expected snapshot.
// A live scope must also cover the final I/O deadline and the full write window.
func (f controlledExecutionFence) requireSubmission(ctx context.Context, q Queryer, window time.Duration) error {
	if f.ProofWindowID != "" {
		current, scope, found, err := controlledProofExecution(ctx, q, f.ProofWindowID, true)
		if err != nil {
			return err
		}
		if !found || current != f {
			return Denied(409, "onboarding_controlled_proof_window_invalid")
		}
		var deadline any
		if until, ok := ctx.Deadline(); ok {
			deadline = until
		} else if window <= 0 {
			return Denied(409, "onboarding_controlled_proof_window_invalid")
		}
		var covered bool
		err = q.QueryRow(ctx, `SELECT least(w.expires_at,c.expires_at,s.expires_at)>
			clock_timestamp()+make_interval(secs=>$4) AND
			($5::timestamptz IS NULL OR least(w.expires_at,c.expires_at,s.expires_at)>=$5::timestamptz)
			FROM test_windows w JOIN group_binding_challenges c ON c.id=$2
			JOIN onboarding_test_scopes s ON s.id=c.controlled_scope_id
			WHERE w.id=$1 AND s.id=$3`, f.ProofWindowID, scope.Challenge, f.ControlledScopeID,
			window.Seconds(), deadline).Scan(&covered)
		if err == pgx.ErrNoRows || err == nil && !covered {
			return Denied(409, "onboarding_controlled_proof_window_invalid")
		}
		return err
	}
	if f.ControlledScopeID == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var deadline any
	if until, ok := ctx.Deadline(); ok {
		deadline = until
	} else if window <= 0 {
		return Denied(409, "controlled_onboarding_scope_inactive")
	}
	var live bool
	err := q.QueryRow(ctx, `SELECT onboarding_test_scope_active(id) AND version=$2
		AND expires_at>clock_timestamp()+make_interval(secs=>$3)
		AND ($4::timestamptz IS NULL OR expires_at>=$4::timestamptz)
		FROM onboarding_test_scopes WHERE id=$1`,
		f.ControlledScopeID, f.ControlledScopeVersion, window.Seconds(), deadline).Scan(&live)
	if err == pgx.ErrNoRows || err == nil && !live {
		return Denied(409, "controlled_onboarding_scope_inactive")
	}
	return err
}

func (f controlledExecutionFence) modelContext(ctx context.Context, q Queryer) (context.Context, context.CancelFunc, error) {
	if f.ProofWindowID != "" {
		return nil, nil, Denied(409, "onboarding_controlled_proof_window_invalid")
	}
	if f.ControlledScopeID == "" {
		return ctx, func() {}, nil
	}
	var expiry time.Time
	err := q.QueryRow(ctx, `SELECT expires_at FROM onboarding_test_scopes
		WHERE id=$1 AND version=$2 AND onboarding_test_scope_active(id)`,
		f.ControlledScopeID, f.ControlledScopeVersion).Scan(&expiry)
	if err == pgx.ErrNoRows {
		return nil, nil, Denied(409, "controlled_onboarding_scope_inactive")
	}
	if err != nil {
		return nil, nil, err
	}
	work, cancel := context.WithDeadline(ctx, expiry)
	if err = work.Err(); err != nil {
		cancel()
		return nil, nil, err
	}
	return work, cancel, nil
}

// Reservations are append-only, including failed and unknown submissions.
// Callers commit this transaction before any external I/O, then revalidate the
// live scope in their final submission fence. This is not execution admission.
func ReserveControlledNativeTx(ctx context.Context, tx pgx.Tx, platform, group, source, key string) error {
	return reserveControlledUsageTx(ctx, tx, platform, group, "native", source, key, 0)
}

// Proof sends have a singleton, one-attempt window of their own. Their consumed
// slot is revalidated here; ordinary actions keep the unchanged shared budget.
func (f controlledExecutionFence) reserveNative(ctx context.Context, tx pgx.Tx, actor, platform, group, source, key string) error {
	if f.ProofWindowID != "" {
		if source != "native_text" && source != "native_settings" || !ValidID(key) {
			return Denied(409, "onboarding_controlled_proof_window_invalid")
		}
		return f.require(ctx, tx, actor, platform, group)
	}
	return ReserveControlledNativeTx(ctx, tx, platform, group, source, key)
}

// reserveMicros is micro-USDT, not the shared gateway's micro-credit unit.
func ReserveControlledModelTx(ctx context.Context, tx pgx.Tx, platform, group, key string, reserveMicros int64) error {
	return reserveControlledUsageTx(ctx, tx, platform, group, "model", "gateway", key, reserveMicros)
}

// Onboarding already holds the exact persisted challenge authorization before
// a binding exists. This wrapper never derives scope from a roster or group.
func ReserveControlledScopeNativeTx(ctx context.Context, tx pgx.Tx, scopeID, source, key string) error {
	if !ValidID(scopeID) {
		return Denied(400, "controlled_onboarding_scope_invalid")
	}
	var fence controlledExecutionFence
	var platform, group string
	err := tx.QueryRow(ctx, `SELECT s.id::text,s.version,s.platform,s.group_id::text FROM onboarding_test_scopes s
		JOIN native_groups g ON g.id=s.group_id AND g.platform=s.platform
		JOIN accounts a ON a.id=s.account_id JOIN accounts c ON c.id=s.claimant_account_id
		JOIN users u ON u.id=s.actor_id JOIN tenants t ON t.id=s.tenant_id
		WHERE s.id=$1 FOR SHARE OF s,g,a,c,u,t`, scopeID).
		Scan(&fence.ControlledScopeID, &fence.ControlledScopeVersion, &platform, &group)
	if err == pgx.ErrNoRows {
		return Denied(409, "controlled_onboarding_scope_inactive")
	}
	if err != nil {
		return err
	}
	return reserveControlledScopeUsageTx(ctx, tx, fence, platform, group, "native", source, key, 0)
}

func reserveControlledUsageTx(ctx context.Context, tx pgx.Tx, platform, group, kind, source, key string, amount int64) error {
	if group == "" {
		return nil
	}
	fence, err := controlledExecutionScope(ctx, tx, "", platform, group)
	if err != nil || fence.ControlledScopeID == "" {
		return err
	}
	return reserveControlledScopeUsageTx(ctx, tx, fence, platform, group, kind, source, key, amount)
}

func reserveControlledScopeUsageTx(ctx context.Context, tx pgx.Tx, fence controlledExecutionFence,
	platform, group, kind, source, key string, amount int64) error {
	if source == "" || len(source) > 100 || key == "" || len(key) > 200 ||
		kind == "model" && amount < 1 {
		return Denied(400, "controlled_onboarding_usage_invalid")
	}
	// Serialize every source and both budgets on the same scope. The advisory
	// lock avoids upgrading a SHARE scope lock held by a caller to UPDATE.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,798568))`, fence.ControlledScopeID); err != nil {
		return err
	}
	var live bool
	err := tx.QueryRow(ctx, `SELECT onboarding_test_scope_active(id) AND version=$2
		FROM onboarding_test_scopes WHERE id=$1`, fence.ControlledScopeID, fence.ControlledScopeVersion).Scan(&live)
	if err == pgx.ErrNoRows || err == nil && !live {
		return Denied(409, "controlled_onboarding_scope_inactive")
	}
	if err != nil {
		return err
	}
	var previous int64
	var previousVersion int64
	err = tx.QueryRow(ctx, `SELECT reserved_micro_usdt,scope_version FROM controlled_onboarding_usage
		WHERE scope_id=$1 AND kind=$2 AND source=$3 AND operation_key=$4`,
		fence.ControlledScopeID, kind, source, key).Scan(&previous, &previousVersion)
	if err == nil {
		if previousVersion != fence.ControlledScopeVersion {
			return Denied(409, "controlled_onboarding_scope_changed")
		}
		if previous != amount {
			return Denied(409, "idempotency_conflict")
		}
		return nil
	}
	if err != pgx.ErrNoRows {
		return err
	}
	var count int
	var used int64
	err = tx.QueryRow(ctx, `SELECT count(*)::int,coalesce(sum(reserved_micro_usdt),0)::bigint
		FROM controlled_onboarding_usage WHERE scope_id=$1 AND kind=$2`,
		fence.ControlledScopeID, kind).Scan(&count, &used)
	if err != nil {
		return err
	}
	if count >= 2 || kind == "model" && amount > 20_000-used {
		return Denied(409, "controlled_onboarding_"+kind+"_budget_exhausted")
	}
	_, err = tx.Exec(ctx, `INSERT INTO controlled_onboarding_usage
		(scope_id,scope_version,platform,group_id,kind,source,operation_key,reserved_micro_usdt)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		fence.ControlledScopeID, fence.ControlledScopeVersion, platform, group, kind, source, key, amount)
	return err
}

func ReadControlledUsage(ctx context.Context, q Queryer, scopeID string) (native, model int, reservedMicros int64, err error) {
	if !ValidID(scopeID) {
		return 0, 0, 0, Denied(400, "controlled_onboarding_scope_invalid")
	}
	err = q.QueryRow(ctx, `SELECT count(*) FILTER(WHERE kind='native')::int,
		count(*) FILTER(WHERE kind='model')::int,coalesce(sum(reserved_micro_usdt),0)::bigint
		FROM controlled_onboarding_usage WHERE scope_id=$1`, scopeID).Scan(&native, &model, &reservedMicros)
	return
}

func controlledModelMicroUSDT(microCredits int64) int64 {
	// One credit is 0.001 USDT; round up without overflowing an int64.
	return microCredits/1000 + min(1, microCredits%1000)
}

// Directory queries use the same liveness/owner contract as selected reads.
// Aliases o/b must refer to ownership/binding; $4 is the website actor.
const controlledBindingVisibleSQL = `(coalesce(o.proof_kind,'authenticated_message')<>'controlled_test'
	OR o.state='active' AND EXISTS(SELECT 1 FROM onboarding_test_scopes cs
		WHERE cs.id=o.controlled_scope_id AND cs.actor_id=$4::uuid AND cs.group_id=b.group_id
		AND cs.tenant_id=b.tenant_id AND cs.platform=o.platform
		AND cs.account_id=b.primary_account_id AND onboarding_test_scope_active(cs.id)))`
