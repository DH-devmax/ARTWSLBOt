package control

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

type AssistantObservation struct {
	State      string    `json:"state"`
	EventID    int64     `json:"event_id,omitempty"`
	NativeID   string    `json:"native_id,omitempty"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
}
type AssistantReceiptReader func(context.Context, string, string, string, string, string, time.Time) (AssistantObservation, error)

func (s *AssistantService) observeAssistantAction(ctx context.Context, p *AssistantActionPlan) {
	switch p.Status {
	case "succeeded":
		p.EvidenceState = "platform_receipt"
	case "pending_approval", "blocked":
		p.EvidenceState = "awaiting_confirmation"
	case "approved", "submitting":
		p.EvidenceState = "submitting"
	default:
		p.EvidenceState = p.Status
	}
	if p.Action != "send_text" || p.Status != "succeeded" {
		return
	}
	p.Observation = &AssistantObservation{State: "not_observed"}
	if s.ReceiptReader == nil {
		p.Observation.State = "unavailable"
		return
	}
	var client, sender, group string
	var at time.Time
	err := s.DB.Platform.QueryRow(ctx, `SELECT t.client_id::text,t.message_at,a.native_uid,g.native_id
  FROM assistant_message_attempts t JOIN native_setting_operations o ON o.id=t.operation_id
  JOIN assistant_action_plans ap ON ap.operation_id=o.id AND ap.actor_id=o.actor_id AND ap.id::text=o.idempotency_key
  JOIN accounts a ON a.id=o.account_id JOIN native_groups g ON g.id=o.group_id
  WHERE ap.id=$1 AND o.id=$2 AND o.account_id=$3 AND o.group_id=$4 AND t.platform=$5 AND o.kind='send_text' AND o.status='succeeded'`,
		p.ID, p.OperationID, p.Scope.AccountID, p.Scope.GroupID, p.Scope.Platform).Scan(&client, &at, &sender, &group)
	if err != nil {
		if err != pgx.ErrNoRows {
			p.Observation.State = "unavailable"
		}
		return
	}
	p.OccurredAt = at
	observation, err := s.ReceiptReader(ctx, p.Scope.Platform, p.Scope.AccountID, group, client, sender, at)
	if err != nil {
		p.Observation.State = "unavailable"
		return
	}
	if observation.State == "receive_observed" && observation.EventID > 0 && observation.NativeID != "" {
		p.Observation = &observation
		p.EvidenceState = observation.State
	}
}
