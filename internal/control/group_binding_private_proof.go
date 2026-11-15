package control

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// A persisted friend round needs independent private ingress. The default
// incoming_contact flow alone also covers applicants already in the group.
func (s *GroupBindingService) requireBindingPrivateProof(ctx context.Context, q Queryer, c bindingRecord, sender string) error {
	if len(c.Identity) > 0 {
		if s.Vault == nil {
			return Denied(503, "group_binding_native_unavailable")
		}
		identity, err := s.Vault.Open("group-binding-identity:"+c.ID, c.Identity)
		if err != nil {
			return err
		}
		same := string(identity) == sender
		clear(identity)
		if !same {
			return Denied(409, "group_binding_identity_changed")
		}
	}
	if c.FriendFlow != "incoming_contact" {
		return nil
	}
	var friendRound bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_binding_friend_attempts WHERE challenge_id=$1)
		OR EXISTS(SELECT 1 FROM group_binding_outbound_attempts WHERE challenge_id=$1
			AND account_id<>$2 AND test_window_id IS NOT NULL)`, c.ID, c.Account).Scan(&friendRound)
	if err != nil || !friendRound && len(c.Identity) == 0 {
		return err
	}
	if len(c.Identity) == 0 {
		return Denied(409, "group_binding_identity_required")
	}
	if s.Decode == nil {
		return Denied(503, "group_binding_native_unavailable")
	}
	var event bindingIngress
	var self string
	err = q.QueryRow(ctx, `SELECT e.id,e.event_key,d.payload_sealed,e.payload_sealed,a.native_uid
		FROM group_binding_challenges c JOIN accounts a ON a.id=c.account_id
		JOIN native_received_events e ON e.id=c.identity_event_id AND e.account_id=c.account_id
		JOIN native_decoded_events d ON d.event_id=e.id AND d.account_id=e.account_id
		WHERE c.id=$1 AND e.account_id=$2 AND e.kind='private' AND d.scene='private'
		AND d.state='decoded' AND e.received_at>=$3 AND e.received_at<=$4
		AND e.received_at<=clock_timestamp()
		AND d.payload_sealed IS NOT NULL AND e.payload_sealed IS NOT NULL
		AND octet_length(d.payload_sealed)<=16384 AND octet_length(e.payload_sealed)<=16384
		FOR SHARE OF e,d`, c.ID, c.Account, c.CreatedAt, c.ExpiresAt).
		Scan(&event.ID, &event.Key, &event.Projection, &event.Source, &self)
	if err == pgx.ErrNoRows {
		return Denied(409, "group_binding_source_expired")
	}
	if err != nil {
		return err
	}
	defer clear(event.Projection)
	defer clear(event.Source)
	message, err := s.Decode(c.Platform, c.Account, self, event.Key, event.ID, event.Projection, event.Source)
	if err != nil || !validBindingMessage(message, self) || !message.Private ||
		!bindingTokenMatches(c.PrivateTokenHash, message.Text, "群接入 ") {
		return Denied(409, "group_binding_source_expired")
	}
	if message.Sender != sender {
		return Denied(409, "group_binding_identity_changed")
	}
	return nil
}
