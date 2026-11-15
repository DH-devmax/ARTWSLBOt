package control

import (
	"context"
	"encoding/json"

	"wangshangliao/internal/assistant"
)

type assistantPlanBatchKey struct{}
type assistantConfigRecord struct {
	AssistantPlan
	Sealed             []byte `json:"sealed"`
	StoredAccountEpoch int64  `json:"stored_account_epoch"`
	StoredBindingEpoch int64  `json:"stored_binding_epoch"`
}
type assistantActionRecord struct {
	ID           string `json:"id"`
	Sealed       []byte `json:"sealed"`
	Fingerprint  string `json:"authorization_hash"`
	Status       string `json:"status"`
	Code         string `json:"result_code"`
	Operation    string `json:"operation_id"`
	Mode         string `json:"execution_mode"`
	Version      int64  `json:"version"`
	AccountEpoch int64  `json:"account_epoch"`
	BindingEpoch int64  `json:"binding_epoch"`
}
type assistantPlanBatch struct {
	actor, platform string
	configs         map[string]assistantConfigRecord
	actions         map[string]assistantActionRecord
}

// Only one transcript request owns these sealed snapshots. Authorization and
// final delivery checks remain live; this is never a cross-request cache.
func (s *AssistantService) batchAssistantPlans(ctx context.Context, session AssistantSession, configIDs, actionIDs []string) (context.Context, error) {
	b := &assistantPlanBatch{actor: session.Actor.ID, platform: session.Scope.Platform, configs: map[string]assistantConfigRecord{}, actions: map[string]assistantActionRecord{}}
	if len(configIDs)+len(actionIDs) > AssistantHistoryLimit*assistant.MaxTools {
		return ctx, Denied(503, "assistant_invalid_event")
	}
	if len(configIDs)+len(actionIDs) == 0 {
		return context.WithValue(ctx, assistantPlanBatchKey{}, b), nil
	}
	err := s.read(ctx, session.Actor, session.Scope.Platform, func(q Queryer) error {
		if len(configIDs) > 0 {
			var raw []byte
			if e := q.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (SELECT p.id,p.session_id,p.account_id,p.group_id,p.base_version,
    p.account_epoch AS stored_account_epoch,p.binding_epoch AS stored_binding_epoch,p.digest,p.version,p.status,p.expires_at,
    encode(p.payload_sealed,'base64') AS sealed,coalesce(p.execution_id::text,'') AS execution_id,coalesce(p.committed_version,0) AS committed_version,p.execution_mode,g.name AS group_name
    FROM assistant_plans p JOIN native_groups g ON g.id=p.group_id AND g.platform=p.domain
    WHERE p.id=ANY($1::uuid[]) AND p.actor_id=$2 AND p.domain=$3 AND p.session_id=$4) x`, configIDs, b.actor, b.platform, session.ID).Scan(&raw); e != nil {
				return e
			}
			var rows []assistantConfigRecord
			if e := json.Unmarshal(raw, &rows); e != nil {
				return e
			}
			for _, r := range rows {
				r.AccountEpoch, r.BindingEpoch = r.StoredAccountEpoch, r.StoredBindingEpoch
				b.configs[r.ID] = r
			}
		}
		if len(actionIDs) > 0 {
			var raw []byte
			if e := q.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (SELECT id,encode(payload_sealed,'base64') AS sealed,authorization_hash,status,result_code,version,
    coalesce(operation_id::text,'') AS operation_id,account_epoch,binding_epoch,execution_mode FROM assistant_action_plans
    WHERE id=ANY($1::uuid[]) AND actor_id=$2 AND domain=$3 AND session_id=$4) x`, actionIDs, b.actor, b.platform, session.ID).Scan(&raw); e != nil {
				return e
			}
			var rows []assistantActionRecord
			if e := json.Unmarshal(raw, &rows); e != nil {
				return e
			}
			for _, r := range rows {
				b.actions[r.ID] = r
			}
		}
		return nil
	})
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, assistantPlanBatchKey{}, b), nil
}

func cachedAssistantPlans(ctx context.Context, actor, platform string) *assistantPlanBatch {
	b, _ := ctx.Value(assistantPlanBatchKey{}).(*assistantPlanBatch)
	if b == nil || b.actor != actor || b.platform != platform {
		return nil
	}
	return b
}
