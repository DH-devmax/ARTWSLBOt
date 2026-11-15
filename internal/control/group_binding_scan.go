package control

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// Run is one bounded scan loop for the service, not one goroutine per group.
// The caller runs Recover before starting it and waits for done on shutdown.
func (s *GroupBindingService) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				work, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := s.ScanOnce(work)
				cancel()
				if err != nil && ctx.Err() == nil {
					slog.Warn("group binding scan unavailable")
				}
			}
		}
	}()
	return done
}

// ScanOnce reads durable authenticated projections only. Observed event IDs,
// rather than a high-water mark, allow a lower-ID transaction to commit later.
func (s *GroupBindingService) ScanOnce(ctx context.Context) error {
	if err := s.SweepControlledScopes(ctx); err != nil {
		return err
	}
	if s.Decode == nil || s.Native == nil || s.Vault == nil {
		return Denied(503, "group_binding_native_unavailable")
	}
	// A closed onboarding surface has no work to lock or update. This is a
	// discovery hint only; the transaction below rechecks every live challenge.
	var pending bool
	err := s.DB.Platform.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM group_binding_challenges c
		WHERE c.state='waiting' AND c.expires_at>clock_timestamp()
			AND (c.controlled_scope_id IS NULL OR onboarding_test_scope_active(c.controlled_scope_id))
		OR c.state IN ('waiting','proven','review') AND c.reservation_until<=clock_timestamp()
			AND NOT EXISTS(SELECT 1 FROM customer_group_accept_attempts a WHERE a.challenge_id=c.id
				AND a.status IN ('prepared','submitting','unknown'))
			AND NOT group_binding_friend_pending(c.id))`).
		Scan(&pending)
	if err != nil || !pending {
		return err
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = expireBindingChallenges(ctx, tx); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id::text FROM group_binding_challenges WHERE state='waiting'
		AND expires_at>clock_timestamp()
		AND (controlled_scope_id IS NULL OR onboarding_test_scope_active(controlled_scope_id))
		ORDER BY last_scan_at,id LIMIT 10 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE group_binding_challenges SET last_scan_at=clock_timestamp() WHERE id=ANY($1::uuid[])`, ids); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	for _, id := range ids {
		if err = s.scanChallenge(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

type bindingIngress struct {
	ID                 int64
	Key                string
	Projection, Source []byte
}

func (s *GroupBindingService) scanChallenge(ctx context.Context, id string) error {
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	c, err := loadBindingRecord(ctx, tx, id, false)
	if err != nil {
		return err
	}
	if c.State != "waiting" || !c.ExpiresAt.After(time.Now()) {
		return nil
	}
	if err = requireControlledChallenge(ctx, tx, c); err != nil {
		var denied *Failure
		if errors.As(err, &denied) {
			return nil
		}
		return err
	}
	if err = bindingApplicant(ctx, tx, c, false); err != nil {
		return s.blockChallenge(ctx, c.ID, "group_binding_scope_changed")
	}
	self, err := s.connection(ctx, tx, c, nil)
	if err != nil {
		return s.blockChallenge(ctx, c.ID, "group_binding_receiver_unavailable")
	}
	rows, err := tx.Query(ctx, `SELECT d.event_id,e.event_key,d.payload_sealed,e.payload_sealed
		FROM native_received_events e JOIN native_decoded_events d ON d.event_id=e.id AND d.account_id=e.account_id
		WHERE e.account_id=$1 AND e.received_at>=$2 AND e.received_at<=$3
		AND d.state='decoded' AND d.scene IN ('group','private') AND e.kind IN ('group','private')
		AND d.payload_sealed IS NOT NULL AND e.payload_sealed IS NOT NULL
		AND octet_length(d.payload_sealed)<=16384 AND octet_length(e.payload_sealed)<=16384
		AND NOT EXISTS(SELECT 1 FROM group_binding_observations o WHERE o.challenge_id=$4 AND o.event_id=e.id)
		ORDER BY e.id LIMIT 64`, c.Account, c.CreatedAt, c.ExpiresAt, c.ID)
	if err != nil {
		return err
	}
	events := []bindingIngress{}
	for rows.Next() {
		var event bindingIngress
		if err = rows.Scan(&event.ID, &event.Key, &event.Projection, &event.Source); err != nil {
			rows.Close()
			return err
		}
		events = append(events, event)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Do not retain locks or a database connection while the native role reader
	// uses its existing account queue and global read slots.
	if err = tx.Rollback(ctx); err != nil {
		return err
	}
	for _, event := range events {
		message, decodeErr := s.Decode(c.Platform, c.Account, self, event.Key, event.ID, event.Projection, event.Source)
		clear(event.Projection)
		clear(event.Source)
		valid := decodeErr == nil && validBindingMessage(message, self)
		prefix := "群绑定 "
		digest := c.TokenHash
		if message.Private {
			prefix = "群接入 "
			digest = c.PrivateTokenHash
		}
		matches := valid && bindingTokenMatches(digest, message.Text, prefix)
		if matches {
			if senderErr := bindingChallengeSender(ctx, s.DB.Platform, c, message.Sender); senderErr != nil {
				if err = s.observeEvent(ctx, c.ID, event.ID); err != nil {
					return err
				}
				return s.blockChallenge(ctx, c.ID, bindingProofReason(senderErr))
			}
		}
		if matches {
			if err = s.observeEvent(ctx, c.ID, event.ID); err != nil {
				return err
			}
			if message.Private {
				if err = s.recordBindingIdentity(ctx, c, message.Sender, event.ID); err != nil {
					return s.blockChallenge(ctx, c.ID, bindingProofReason(err))
				}
				return nil
			}
			roles, readErr := s.Native.BindingRoles(ctx, c.scope(message.Group, message.Sender))
			if readErr == nil {
				p := bindingProof{Group: message.Group, Sender: message.Sender, Owner: roles.NativeOwnerID,
					SenderRole: roles.SenderRole, RobotRole: roles.RobotRole, Event: event.ID}
				readErr = s.recordProof(ctx, c, p, roles)
			}
			// One authenticated event gets one native role read. Failed reads
			// require a fresh owner message; polling must not hammer the API.
			if readErr != nil {
				return s.blockChallenge(ctx, c.ID, bindingProofReason(readErr))
			}
			return nil
		}
		if err = s.observeEvent(ctx, c.ID, event.ID); err != nil {
			return err
		}
	}
	return nil
}

// Only fixed control codes may reach the progress view; native errors and
// upstream response bodies are never persisted as claim diagnostics.
func bindingProofReason(err error) string {
	var failure *Failure
	if errors.As(err, &failure) {
		switch failure.Code {
		case "group_binding_native_role_required", "group_binding_native_role_changed",
			"group_binding_identity_changed", "group_binding_identity_required", "group_binding_source_expired",
			"group_binding_group_changed", "group_binding_group_name_invalid",
			"group_binding_pool_identity",
			"controlled_onboarding_scope_inactive", "controlled_onboarding_claimant_mismatch", "controlled_onboarding_role_changed",
			"group_binding_restore_not_allowed", "group_binding_transfer_owner_required",
			"group_binding_challenge_pending", "group_binding_scope_changed", "group_binding_capacity_exceeded",
			"group_binding_receiver_unavailable", "group_binding_challenge_expired":
			return failure.Code
		case "permission_denied":
			return "group_binding_scope_changed"
		}
	}
	return "group_binding_native_proof_unavailable"
}

func validBindingMessage(m OnboardingMessage, self string) bool {
	return (m.Private && m.Group == "" && m.Recipient == self ||
		!m.Private && onboardingLabel(m.Group, 200) && m.Recipient == m.Group) && onboardingLabel(m.Sender, 200) &&
		m.Sender != self && onboardingLabel(m.ClientID, 300) &&
		len(m.Text) <= 160 && utf8.ValidString(m.Text)
}

func (s *GroupBindingService) observeEvent(ctx context.Context, challenge string, event int64) error {
	_, err := s.DB.Platform.Exec(ctx, `INSERT INTO group_binding_observations(challenge_id,event_id)
		VALUES($1,$2) ON CONFLICT DO NOTHING`, challenge, event)
	return err
}

func (s *GroupBindingService) blockChallenge(ctx context.Context, id, reason string) error {
	_, err := s.DB.Platform.Exec(ctx, `UPDATE group_binding_challenges SET blocked_reason=$2 WHERE id=$1 AND state='waiting'`, id, reason)
	return err
}

func (s *GroupBindingService) recordProof(ctx context.Context, expected bindingRecord, p bindingProof, roles GroupBindingRoleEvidence) error {
	if !bindingRolesMatch(p, roles) {
		return Denied(409, "group_binding_native_role_required")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return err
	}
	c, err := loadBindingRecord(ctx, tx, expected.ID, true)
	if err != nil {
		return err
	}
	if c.State != "waiting" || c.Version != expected.Version || !c.ExpiresAt.After(time.Now()) {
		return Denied(409, "group_binding_challenge_expired")
	}
	if err = bindingApplicant(ctx, tx, c, true); err != nil {
		return err
	}
	if err = s.applicantPermission(ctx, tx, c); err != nil {
		return err
	}
	if err = s.controlledRoleProof(ctx, tx, c, p); err != nil {
		return err
	}
	if err = s.checkOutboundSender(ctx, tx, c, p.Sender); err != nil {
		return err
	}
	if err = s.checkContactSender(ctx, tx, c, p.Sender); err != nil {
		return err
	}
	if _, err = s.connection(ctx, tx, c, &roles.GroupBindingConnectionEvidence); err != nil {
		return err
	}
	if err = s.requireBindingPrivateProof(ctx, tx, c, p.Sender); err != nil {
		return err
	}
	name := roles.GroupName
	if !onboardingLabel(name, 200) {
		return Denied(409, "group_binding_group_name_invalid")
	}
	var group string
	err = tx.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name) VALUES($1,$2,$3)
		ON CONFLICT(platform,native_id) DO UPDATE SET name=excluded.name RETURNING id::text`, c.Platform, p.Group, name).Scan(&group)
	if err != nil {
		return err
	}
	if c.Group != "" && c.Group != group {
		return Denied(409, "group_binding_group_changed")
	}
	var tenant string
	var version int64
	var active bool
	err = tx.QueryRow(ctx, `SELECT tenant_id::text,version,active FROM group_bindings WHERE group_id=$1 FOR UPDATE`, group).
		Scan(&tenant, &version, &active)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	transferFrom := ""
	if err == nil {
		if tenant != c.Tenant {
			if p.SenderRole != "owner" || c.BindingVersion != 0 && c.BindingVersion != version {
				return Denied(409, "group_binding_transfer_owner_required")
			}
			transferFrom = tenant
		} else if c.Group == "" && active || c.BindingVersion != 0 && c.BindingVersion != version {
			return Denied(409, "group_binding_restore_not_allowed")
		} else if active && c.ControlledScope != "" {
			// Received proof does not require the claimant's project connection;
			// native submission windows check that independently.
			var claimant string
			if err = tx.QueryRow(ctx, `SELECT claimant_account_id::text FROM onboarding_test_scopes
				WHERE id=$1 AND actor_id=$2 AND tenant_id=$3 AND platform=$4 AND group_id=$5
				AND account_id=$6 AND onboarding_test_scope_active(id) FOR SHARE`,
				c.ControlledScope, c.Applicant, c.Tenant, c.Platform, group, c.Account).Scan(&claimant); err != nil {
				if err == pgx.ErrNoRows {
					return Denied(409, "controlled_onboarding_scope_inactive")
				}
				return err
			}
			if _, err = loadOnboardingControlledBinding(ctx, tx, group, c.Tenant, claimant, c.Account, c.Platform); err != nil {
				return err
			}
		}
	}
	if err == pgx.ErrNoRows {
		version = 0
	}
	if err = bindingCapacity(ctx, tx, c.Tenant, c.Platform, c.Account, group, c.ID); err != nil {
		return err
	}
	var other bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_binding_challenges WHERE group_id=$1 AND id<>$2
		AND state IN ('waiting','proven','review'))`, group, c.ID).Scan(&other); err != nil {
		return err
	}
	if other {
		return Denied(409, "group_binding_challenge_pending")
	}
	raw, _ := json.Marshal(p)
	defer clear(raw)
	sealed, err := s.Vault.Seal("group-binding-proof:"+c.ID, raw)
	if err != nil {
		return err
	}
	if err = completeUnsubmittedContact(ctx, tx, c); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE group_binding_challenges SET group_id=$2,binding_version=$3,proof_event_id=$4,
		proof_sealed=$5,proof_role=$6,state='proven',version=version+1,blocked_reason='',
		transfer_from_tenant_id=nullif($7,'')::uuid WHERE id=$1`,
		c.ID, group, version, p.Event, sealed, p.SenderRole, transferFrom)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
		VALUES($1,$2,$3,$4,'native') ON CONFLICT(account_id,group_id) DO UPDATE SET role=excluded.role,
		observed_at=excluded.observed_at,observation_source='native' WHERE account_groups.observed_at<excluded.observed_at`,
		c.Account, group, roles.RobotRole, roles.ObservedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *GroupBindingService) recordBindingIdentity(ctx context.Context, expected bindingRecord, sender string, event int64) error {
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	c, err := loadBindingRecord(ctx, tx, expected.ID, true)
	if err != nil {
		return err
	}
	if c.State != "waiting" || !c.ExpiresAt.After(time.Now()) {
		return Denied(409, "group_binding_challenge_expired")
	}
	if err = bindingApplicant(ctx, tx, c, true); err != nil {
		return err
	}
	if err = bindingChallengeSender(ctx, tx, c, sender); err != nil {
		return err
	}
	if err = s.checkOutboundSender(ctx, tx, c, sender); err != nil {
		return err
	}
	if err = s.checkContactSender(ctx, tx, c, sender); err != nil {
		return err
	}
	if _, err = s.connection(ctx, tx, c, nil); err != nil {
		return err
	}
	if len(c.Identity) > 0 {
		plain, err := s.Vault.Open("group-binding-identity:"+c.ID, c.Identity)
		if err != nil {
			return err
		}
		same := string(plain) == sender
		clear(plain)
		if !same {
			return Denied(409, "group_binding_identity_changed")
		}
		return nil
	}
	sealed, err := s.Vault.Seal("group-binding-identity:"+c.ID, []byte(sender))
	if err != nil {
		return err
	}
	if err = completeUnsubmittedContact(ctx, tx, c); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE group_binding_challenges SET identity_event_id=$2,identity_sealed=$3,
		version=version+1,blocked_reason='' WHERE id=$1`, c.ID, event, sealed)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
