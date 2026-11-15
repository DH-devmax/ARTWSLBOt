package control

import (
	"context"
	"strings"
)

// PendingConversationText exposes only the original actor's unresolved text
// operation IDs. Expired grants cannot hide them or become dispatch authority.
func (s *NativeTextService) PendingConversationText(ctx context.Context, ref SessionRef,
	account, conversation string) ([]TextOperation, error) {
	if !ValidID(account) || len(conversation) != 64 ||
		strings.Trim(conversation, "0123456789abcdef") != "" {
		return nil, Denied(400, "invalid_operation")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	actor, err := sessionActor(ctx, tx, ref, false)
	if err != nil {
		return nil, err
	}
	if actor.Role != "superadmin" {
		return nil, Denied(403, "superadmin_required")
	}
	rows, err := tx.Query(ctx, `SELECT o.id::text,o.status,o.result_code FROM operations o
		JOIN accounts a ON a.id=o.account_id
		WHERE o.actor_id=$1 AND o.account_id=$2 AND o.conversation_key=$3
		AND o.kind='send_text' AND o.status IN('queued','running','unknown')
		AND (EXISTS(SELECT 1 FROM native_text_attempts t WHERE t.operation_id=o.id)
			OR EXISTS(SELECT 1 FROM lajiaoliao_media_attempts t WHERE t.operation_id=o.id
				AND a.platform='lajiaoliao' AND t.native_sender=a.native_uid))
		ORDER BY CASE o.status WHEN 'unknown' THEN 0 WHEN 'running' THEN 1 ELSE 2 END,
			o.created_at DESC LIMIT 10`, actor.ID, account, conversation)
	if err != nil {
		return nil, err
	}
	out := make([]TextOperation, 0, 10)
	for rows.Next() {
		var item TextOperation
		if err = rows.Scan(&item.ID, &item.Status, &item.ResultCode); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}
