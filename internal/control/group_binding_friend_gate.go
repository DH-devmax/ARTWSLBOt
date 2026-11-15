package control

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
)

type GroupBindingFriendGate struct {
	Platform  string   `json:"platform"`
	AccountID string   `json:"account_id"`
	State     string   `json:"state"`
	Version   int64    `json:"version"`
	Evidence  []string `json:"evidence_refs"`
}

type GroupBindingFriendGateInput struct {
	State    string   `json:"state"`
	Version  int64    `json:"version"`
	Evidence []string `json:"evidence_refs"`
}

func requireBindingFriendGate(ctx context.Context, q Queryer, platform, account string, expected int64) (int64, error) {
	var state string
	var version int64
	err := q.QueryRow(ctx, `SELECT state,version FROM group_binding_friend_gates
		WHERE platform=$1 AND account_id=$2 FOR SHARE`, platform, account).Scan(&state, &version)
	if err == pgx.ErrNoRows || err == nil && (state != "open" || expected != 0 && version != expected) {
		return 0, Denied(409, "group_binding_friend_admission_closed")
	}
	return version, err
}

func bindingFriendAdmin(ctx context.Context, tx pgx.Tx, ref SessionRef, account string) (Actor, error) {
	a, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return a, err
	}
	if a.Role != "superadmin" {
		return a, Denied(403, "superadmin_required")
	}
	if !bindingPlatform(ref.assistantPlatform()) || !ValidID(account) {
		return a, Denied(400, "group_binding_friend_gate_invalid")
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT id::text FROM accounts WHERE id=$1 AND platform=$2 AND workspace=$2
		AND purpose='management' FOR SHARE`, account, ref.assistantPlatform()).Scan(&id)
	if err == pgx.ErrNoRows {
		return a, Denied(404, "account_not_found")
	}
	return a, err
}

func (s *GroupBindingService) FriendGate(ctx context.Context, ref SessionRef, account string) (GroupBindingFriendGate, error) {
	out := GroupBindingFriendGate{Platform: ref.assistantPlatform(), AccountID: account, State: "closed", Evidence: []string{}}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = bindingFriendAdmin(ctx, tx, ref, account); err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT state,version,evidence_refs FROM group_binding_friend_gates WHERE platform=$1 AND account_id=$2`,
		out.Platform, account).Scan(&out.State, &out.Version, &out.Evidence)
	if err == pgx.ErrNoRows {
		err = nil
	}
	return out, err
}

// This account-only gate is independent of commercial group admission and of
// controlled/native_debug grants. Missing records always mean closed.
func (s *GroupBindingService) SaveFriendGate(ctx context.Context, ref SessionRef, account string, in GroupBindingFriendGateInput) (GroupBindingFriendGate, error) {
	out := GroupBindingFriendGate{Platform: ref.assistantPlatform(), AccountID: account, State: in.State, Evidence: in.Evidence}
	if in.Version < 0 || !slices.Contains([]string{"closed", "open"}, in.State) ||
		!ValidBusinessEvidenceRefs(in.Evidence) || in.State == "open" && len(in.Evidence) == 0 {
		return out, Denied(400, "group_binding_friend_gate_invalid")
	}
	if out.Evidence == nil {
		out.Evidence = []string{}
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	actor, err := bindingFriendAdmin(ctx, tx, ref, account)
	if err != nil {
		return out, err
	}
	if in.Version == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO group_binding_friend_gates(platform,account_id,state,evidence_refs,updated_by)
			VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING version`,
			out.Platform, account, in.State, out.Evidence, actor.ID).Scan(&out.Version)
	} else {
		err = tx.QueryRow(ctx, `UPDATE group_binding_friend_gates SET state=$3,evidence_refs=$4,updated_by=$5,
			version=version+1,updated_at=clock_timestamp() WHERE platform=$1 AND account_id=$2 AND version=$6 RETURNING version`,
			out.Platform, account, in.State, out.Evidence, actor.ID, in.Version).Scan(&out.Version)
	}
	if err == pgx.ErrNoRows {
		return out, Denied(409, "version_conflict")
	}
	if err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, actor, "", "group_binding_friend_gate_changed", account,
		map[string]any{"platform": out.Platform, "state": out.State, "version": out.Version, "evidence_refs": out.Evidence}); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
