package control

import (
	"context"
	"encoding/json"
	"strings"

	"wangshangliao/internal/assistant"
)

type AssistantTranscriptTurn struct {
	AssistantTurn
	Answer      string             `json:"answer"`
	Cards       []json.RawMessage  `json:"cards"`
	Outcomes    []AssistantOutcome `json:"outcomes"`
	Cursor      int64              `json:"cursor"`
	Unavailable bool               `json:"unavailable,omitempty"`
	Truncated   bool               `json:"truncated,omitempty"`
}

// Transcript projects only this actor's latest bodies in one bounded snapshot.
// Resuming the active turn uses the last persisted event sequence, not a new POST.
func (s *AssistantService) Transcript(ctx context.Context, ref SessionRef, sessionID string) ([]AssistantTranscriptTurn, error) {
	session, err := s.Session(ctx, ref, sessionID, false)
	if err != nil {
		return nil, err
	}
	fingerprint, err := s.authorizationHash(ctx, ref, session.Scope)
	if err != nil {
		return nil, err
	}
	policy, err := assistantExposure(ctx, s.DB.Platform, session.Scope.Platform)
	if err != nil {
		return nil, err
	}
	var raw []byte
	err = s.read(ctx, session.Actor, session.Scope.Platform, func(q Queryer) error {
		return q.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY x.created_at,x.id),'[]') FROM (
			SELECT t.id,t.session_id,t.status,t.result_code,t.deadline,t.created_at,
				encode(t.input_sealed,'base64') AS input_sealed,t.authorization_hash,t.module_policy_version,
				coalesce((SELECT jsonb_agg(jsonb_build_object('seq',e.seq,'kind',e.kind,
					'sealed',encode(e.payload_sealed,'base64')) ORDER BY e.seq)
					FROM assistant_events e WHERE e.turn_id=t.id AND e.actor_id=$2),'[]') AS events
			FROM assistant_turns t WHERE t.session_id=$1 AND t.actor_id=$2 AND t.domain=$4
				AND t.history_pruned_at IS NULL ORDER BY t.created_at DESC,t.id DESC LIMIT $3
		) x`, session.ID, session.Actor.ID, AssistantHistoryLimit, session.Scope.Platform).Scan(&raw)
	})
	if err != nil {
		return nil, err
	}
	var rows []struct {
		AssistantTurn
		InputSealed         []byte `json:"input_sealed"`
		AuthorizationHash   string `json:"authorization_hash"`
		ModulePolicyVersion int64  `json:"module_policy_version"`
		Events              []struct {
			Seq    int64
			Kind   string
			Sealed []byte
		}
	}
	if err = json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	configIDs, actionIDs := []string{}, []string{}
	ids := map[string]bool{}
	for _, row := range rows {
		for _, event := range row.Events {
			if event.Kind != "tool_result" {
				continue
			}
			body, e := s.Vault.Open(eventScope(row.ID, event.Seq), event.Sealed)
			if e != nil {
				return nil, e
			}
			var card struct {
				Type  string
				Data  json.RawMessage
				Scope assistant.Scope
			}
			e = json.Unmarshal(body, &card)
			clear(body)
			if e != nil {
				return nil, Denied(503, "assistant_invalid_event")
			}
			if card.Type != "propose_group_config_change" && card.Type != "propose_group_action" {
				continue
			}
			var planRef struct{ ID string }
			if json.Unmarshal(card.Data, &planRef) != nil {
				return nil, Denied(503, "assistant_invalid_event")
			}
			if card.Scope.Platform == "" {
				card.Scope.Platform = "wsl"
			}
			if card.Scope != session.Scope || !ValidID(planRef.ID) || ids[card.Type+planRef.ID] {
				continue
			}
			ids[card.Type+planRef.ID] = true
			switch card.Type {
			case "propose_group_config_change":
				configIDs = append(configIDs, planRef.ID)
			case "propose_group_action":
				actionIDs = append(actionIDs, planRef.ID)
			}
		}
	}
	ctx, err = s.batchAssistantPlans(ctx, session, configIDs, actionIDs)
	if err != nil {
		return nil, err
	}
	type projection struct {
		outcome  AssistantOutcome
		card     []byte
		proposal bool
	}
	projected := map[string]projection{}
	out := make([]AssistantTranscriptTurn, 0, len(rows))
	for _, row := range rows {
		item := AssistantTranscriptTurn{AssistantTurn: row.AssistantTurn, Cards: []json.RawMessage{}, Outcomes: []AssistantOutcome{}}
		if row.AuthorizationHash != fingerprint || row.ModulePolicyVersion != policy.Version {
			item.Unavailable = true
		}
		if !item.Unavailable && len(row.InputSealed) > 0 {
			body, openErr := s.Vault.Open("assistant:input:"+row.ID, row.InputSealed)
			if openErr != nil {
				return nil, openErr
			}
			item.Input = string(body)
			clear(body)
		}
		var answer strings.Builder
		seenPlans := map[string]bool{}
		for _, event := range row.Events {
			item.Cursor = event.Seq
			if event.Kind != "text_delta" && event.Kind != "tool_result" {
				continue
			}
			body, openErr := s.Vault.Open(eventScope(row.ID, event.Seq), event.Sealed)
			if openErr != nil {
				return nil, openErr
			}
			if event.Kind == "text_delta" {
				if item.Unavailable {
					clear(body)
					continue
				}
				var part struct{ Text string }
				if json.Unmarshal(body, &part) != nil {
					clear(body)
					return nil, Denied(503, "assistant_invalid_event")
				}
				if answer.Len()+len(part.Text) <= assistant.MaxModels*assistant.MaxResultBytes {
					answer.WriteString(part.Text)
				} else {
					item.Truncated = true
				}
			} else {
				var reference struct {
					Type  string
					Data  struct{ ID string }
					Scope assistant.Scope
				}
				_ = json.Unmarshal(body, &reference)
				key := reference.Type + reference.Data.ID
				p, found := projected[key]
				var e error
				if !found || reference.Scope != session.Scope {
					p.outcome, p.card, p.proposal, e = s.projectAssistantOutcome(ctx, ref, session, body)
					if e == nil && p.proposal {
						projected[key] = p
					}
				}
				outcome, current, proposal := p.outcome, p.card, p.proposal
				if e != nil {
					clear(body)
					return nil, e
				}
				if proposal {
					if outcome.PlanID != "" && !seenPlans[outcome.PlanID] && len(item.Outcomes) < assistant.MaxTools {
						seenPlans[outcome.PlanID] = true
						item.Outcomes = append(item.Outcomes, outcome)
					}
					if len(current) == 0 {
						clear(body)
						continue
					}
					clear(body)
					body = append([]byte(nil), current...)
				}
				if !item.Unavailable {
					if len(item.Cards) < assistant.MaxTools {
						item.Cards = append(item.Cards, append(json.RawMessage(nil), body...))
					} else {
						item.Truncated = true
					}
				}
			}
			clear(body)
		}
		item.Answer = answer.String()
		out = append(out, item)
	}
	if _, err = s.Session(ctx, ref, session.ID, false); err != nil {
		return nil, err
	}
	current, err := s.authorizationHash(ctx, ref, session.Scope)
	if err != nil {
		return nil, err
	}
	if current != fingerprint {
		return nil, Denied(403, "assistant_history_scope_changed")
	}
	nowPolicy, err := assistantExposure(ctx, s.DB.Platform, session.Scope.Platform)
	if err != nil {
		return nil, err
	}
	if nowPolicy.Version != policy.Version {
		return nil, Denied(403, "assistant_history_scope_changed")
	}
	return out, nil
}
