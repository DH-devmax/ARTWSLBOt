package control

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/assistant"
)

type assistantSessionSummary struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IsEmpty   bool      `json:"is_empty"`
	Title     string    `json:"title"`
}

func assistantSessionTitle(input string) string {
	title := []rune(strings.Join(strings.Fields(input), " "))
	if len(title) > 32 {
		return string(title[:32]) + "…"
	}
	return string(title)
}

func (s *AssistantService) Sessions(ctx context.Context, ref SessionRef, scope assistant.Scope) (json.RawMessage, error) {
	if scope.Platform == "" {
		scope.Platform = ref.assistantPlatform()
	}
	var resolveErr error
	scope, resolveErr = s.resolveScope(ctx, ref, scope)
	if resolveErr != nil {
		return nil, resolveErr
	}
	a, err := s.authorize(ctx, s.DB.Platform, ref, scope, "assistant:use", false, false)
	if err != nil {
		return nil, err
	}
	fingerprint, err := s.authorizationHash(ctx, ref, scope)
	if err != nil {
		return nil, err
	}
	var raw []byte
	err = s.read(ctx, a, ref.assistantPlatform(), func(q Queryer) error {
		// Legacy empty duplicates remain stored until normal expiry, but only the
		// newest empty session is presented. A pruned first turn is never replaced.
		return q.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
			SELECT id,created_at,expires_at,is_empty,encode(sealed,'base64') AS sealed,fingerprint,first_turn FROM (
				SELECT s.id,s.created_at,s.expires_at,t.id IS NULL AS is_empty,
					coalesce(s.title_sealed,t.input_sealed) AS sealed,
					coalesce(s.title_authorization_hash,t.authorization_hash,'') AS fingerprint,
					CASE WHEN s.title_sealed IS NULL THEN coalesce(t.id::text,'') ELSE '' END AS first_turn,
					row_number() OVER(PARTITION BY (t.id IS NULL) ORDER BY s.created_at DESC,s.id DESC) AS position
				FROM assistant_sessions s LEFT JOIN LATERAL (
					SELECT id,input_sealed,authorization_hash FROM assistant_turns
					WHERE session_id=s.id ORDER BY created_at,id LIMIT 1
				) t ON true WHERE s.actor_id=$1 AND s.domain=$5 AND s.scope_kind=$2
				AND coalesce(s.account_id::text,'')=$3 AND coalesce(s.group_id::text,'')=$4
				AND s.deleted_at IS NULL AND s.expires_at>clock_timestamp()
			) ranked WHERE NOT is_empty OR position=1 ORDER BY created_at DESC,id DESC LIMIT 30
		) x`, a.ID, scope.Kind, scope.AccountID, scope.GroupID, scope.Platform).Scan(&raw)
	})
	if err != nil {
		return nil, err
	}
	var rows []struct {
		assistantSessionSummary
		Sealed      []byte
		Fingerprint string
		FirstTurn   string `json:"first_turn"`
	}
	if err = json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	out := make([]assistantSessionSummary, 0, len(rows))
	for _, row := range rows {
		if row.Fingerprint == fingerprint && len(row.Sealed) > 0 {
			sealScope := "assistant:title:" + row.ID
			if row.FirstTurn != "" {
				sealScope = "assistant:input:" + row.FirstTurn
			}
			title, openErr := s.Vault.Open(sealScope, row.Sealed)
			if openErr != nil {
				return nil, openErr
			}
			row.Title = assistantSessionTitle(string(title))
			clear(title)
		}
		out = append(out, row.assistantSessionSummary)
	}
	current, err := s.authorizationHash(ctx, ref, scope)
	if err != nil {
		return nil, err
	}
	if current != fingerprint {
		return nil, Denied(403, "assistant_history_scope_changed")
	}
	return json.Marshal(out)
}

// Lock a live session until commit, so deletion cannot race a new turn or approval.
func liveAssistantSession(ctx context.Context, q Queryer, id, actor, platform string) error {
	var found string
	err := q.QueryRow(ctx, `SELECT id::text FROM assistant_sessions
		WHERE id=$1 AND actor_id=$2 AND domain=$3 AND deleted_at IS NULL
		AND expires_at>clock_timestamp() FOR SHARE`, id, actor, platform).Scan(&found)
	if err == pgx.ErrNoRows {
		return Denied(404, "assistant_session_not_found")
	}
	return err
}

type AssistantSessionDeletion struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

func (s *AssistantService) DeleteSession(ctx context.Context, ref SessionRef, id string) (AssistantSessionDeletion, error) {
	out := AssistantSessionDeletion{ID: id}
	if !ValidID(id) {
		return out, Denied(404, "assistant_session_not_found")
	}
	a, err := s.actor(ctx, s.DB.Platform, ref, false)
	if err != nil {
		return out, err
	}
	// Include tombstones only here, allowing a lost DELETE response to be reconciled.
	var scope assistant.Scope
	scope.Platform = ref.assistantPlatform()
	err = s.read(ctx, a, scope.Platform, func(q Queryer) error {
		return q.QueryRow(ctx, `SELECT scope_kind,coalesce(account_id::text,''),coalesce(group_id::text,'')
			FROM assistant_sessions WHERE id=$1 AND actor_id=$2 AND domain=$3
			AND expires_at>clock_timestamp()`, id, a.ID, scope.Platform).
			Scan(&scope.Kind, &scope.AccountID, &scope.GroupID)
	})
	if err == pgx.ErrNoRows {
		return out, Denied(404, "assistant_session_not_found")
	}
	if err != nil {
		return out, err
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	// Approval/revocation lifecycle first, then the actor's create-session/turn lock.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,798441))`, a.ID); err != nil {
		return out, err
	}
	if _, err = s.authorize(ctx, tx, ref, scope, "assistant:use", false, true); err != nil {
		return out, err
	}
	var deleted bool
	err = tx.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM assistant_sessions
		WHERE id=$1 AND actor_id=$2 AND domain=$3 AND expires_at>clock_timestamp() FOR UPDATE`,
		id, a.ID, scope.Platform).Scan(&deleted)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "assistant_session_not_found")
	}
	if err != nil {
		return out, err
	}
	if !deleted {
		var busy bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM assistant_turns WHERE session_id=$1
			AND status IN('queued','running')) OR EXISTS(SELECT 1 FROM assistant_action_plans
			WHERE session_id=$1 AND status IN('approved','submitting'))`, id).Scan(&busy); err != nil {
			return out, err
		}
		if busy {
			return out, Denied(409, "assistant_session_busy")
		}
		if _, err = tx.Exec(ctx, `UPDATE assistant_sessions SET deleted_at=clock_timestamp(),
			title_sealed=NULL,title_authorization_hash=NULL WHERE id=$1`, id); err != nil {
			return out, err
		}
		if _, err = tx.Exec(ctx, `UPDATE assistant_turns SET input_sealed=NULL,target_sealed=NULL,
			history_pruned_at=coalesce(history_pruned_at,clock_timestamp()) WHERE session_id=$1`, id); err != nil {
			return out, err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM assistant_events WHERE turn_id IN
			(SELECT id FROM assistant_turns WHERE session_id=$1)`, id); err != nil {
			return out, err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM assistant_target_choices WHERE session_id=$1`, id); err != nil {
			return out, err
		}
		if _, err = tx.Exec(ctx, `UPDATE assistant_plans SET status='cancelled',version=version+1
			WHERE session_id=$1 AND status='pending_approval'`, id); err != nil {
			return out, err
		}
		if _, err = tx.Exec(ctx, `UPDATE assistant_action_plans SET status='cancelled',version=version+1
			WHERE session_id=$1 AND status IN('pending_approval','blocked')`, id); err != nil {
			return out, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
			VALUES(nullif($1,'')::uuid,$2,'assistant_session_deleted',$3,
			jsonb_build_object('platform',$4::text))`, a.TenantID, a.ID, id, scope.Platform); err != nil {
			return out, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return out, Denied(503, "assistant_commit_unknown")
	}
	out.Deleted = true
	return out, nil
}
