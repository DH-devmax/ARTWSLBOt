package control

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
)

func requireOutboundFriendGate(ctx context.Context, q Queryer, platform, account string, expected int64) (int64, error) {
	var state string
	var version int64
	err := q.QueryRow(ctx, `SELECT state,version FROM group_binding_outbound_gates
		WHERE platform=$1 AND account_id=$2 FOR SHARE`, platform, account).Scan(&state, &version)
	if err == pgx.ErrNoRows || err == nil && (state != "open" || expected != 0 && version != expected) {
		return 0, Denied(409, "group_binding_outbound_admission_closed")
	}
	return version, err
}

func (s *GroupBindingService) OutboundFriendGate(ctx context.Context, ref SessionRef, account string) (GroupBindingFriendGate, error) {
	out := GroupBindingFriendGate{Platform: ref.assistantPlatform(), AccountID: account, State: "closed", Evidence: []string{}}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = bindingFriendAdmin(ctx, tx, ref, account); err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT state,version,evidence_refs FROM group_binding_outbound_gates
		WHERE platform=$1 AND account_id=$2`, out.Platform, account).Scan(&out.State, &out.Version, &out.Evidence)
	if err == pgx.ErrNoRows {
		err = nil
	}
	return out, err
}

func (s *GroupBindingService) SaveOutboundFriendGate(ctx context.Context, ref SessionRef, account string, in GroupBindingFriendGateInput) (GroupBindingFriendGate, error) {
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
		err = tx.QueryRow(ctx, `INSERT INTO group_binding_outbound_gates(platform,account_id,state,evidence_refs,updated_by)
			VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING version`,
			out.Platform, account, in.State, out.Evidence, actor.ID).Scan(&out.Version)
	} else {
		err = tx.QueryRow(ctx, `UPDATE group_binding_outbound_gates SET state=$3,evidence_refs=$4,updated_by=$5,
			version=version+1,updated_at=clock_timestamp()
			WHERE platform=$1 AND account_id=$2 AND version=$6 RETURNING version`,
			out.Platform, account, in.State, out.Evidence, actor.ID, in.Version).Scan(&out.Version)
	}
	if err == pgx.ErrNoRows {
		return out, Denied(409, "version_conflict")
	}
	if err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, actor, "", "group_binding_outbound_gate_changed", account,
		map[string]any{"platform": out.Platform, "state": out.State, "version": out.Version, "evidence_refs": out.Evidence}); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
