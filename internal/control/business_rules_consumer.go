package control

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/features"
)

// Recover never repeats an attempt whose native result may be unknown.
func (s *BusinessRulesService) Recover(ctx context.Context) error {
	_, err := s.DB.Platform.Exec(ctx, `UPDATE business_rule_jobs SET status='expired',
		result_code='business_task_expired',updated_at=clock_timestamp()
		WHERE status='queued' AND expires_at<=clock_timestamp();
		UPDATE business_rule_jobs j SET
		status=CASE WHEN EXISTS(SELECT 1 FROM business_rule_attempts a WHERE a.job_id=j.id)
			THEN 'unknown' WHEN j.expires_at<=clock_timestamp() THEN 'expired'
			WHEN EXISTS(SELECT 1 FROM business_rule_jobs pending WHERE pending.group_id=j.group_id
				AND pending.id<>j.id AND pending.status='queued') THEN 'superseded' ELSE 'queued' END,
		result_code='business_process_restarted',updated_at=clock_timestamp()
		WHERE j.status IN('executing','submitting')`)
	return err
}

// Run owns one bounded poller and at most four workers, independent of the
// number of groups, subscribers or receive connections.
func (s *BusinessRulesService) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		close(done)
		return done
	}
	s.running, s.active = true, make(map[string]bool, businessWorkers)
	s.mu.Unlock()
	go func() {
		defer close(done)
		var workers sync.WaitGroup
		defer func() {
			workers.Wait()
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		var after string
		round := 0
		for ctx.Err() == nil {
			work, cancel := context.WithTimeout(ctx, time.Second)
			var pending bool
			err := s.DB.Platform.QueryRow(work, `SELECT
				EXISTS(SELECT 1 FROM group_business_runtime WHERE enabled)
				OR EXISTS(SELECT 1 FROM business_rule_jobs WHERE status='queued')`).Scan(&pending)
			cancel()
			// An empty poll never starts a transaction or touches message
			// sources. Changes are discovered on the next existing timer tick.
			if err == nil && pending {
				work, cancel = context.WithTimeout(ctx, time.Second)
				groups, err := s.groups(work, after)
				cancel()
				if err == nil {
					for _, group := range groups {
						after = group
						work, cancel = context.WithTimeout(ctx, 8*time.Second)
						_ = s.discover(work, group, false)
						cancel()
					}
				}
				work, cancel = context.WithTimeout(ctx, 2*time.Second)
				ids, _ := s.dispatchable(work)
				cancel()
				for _, id := range ids {
					s.mu.Lock()
					if s.active[id] || len(s.active) >= businessWorkers {
						s.mu.Unlock()
						continue
					}
					s.active[id] = true
					s.mu.Unlock()
					workers.Add(1)
					go func(id string) {
						defer workers.Done()
						defer func() { s.mu.Lock(); delete(s.active, id); s.mu.Unlock() }()
						_ = s.execute(ctx, id)
					}(id)
				}
			}
			round++
			if round%60 == 0 {
				work, cancel = context.WithTimeout(ctx, time.Second)
				_ = s.Prune(work)
				cancel()
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}

func (s *BusinessRulesService) groups(ctx context.Context, after string) ([]string, error) {
	rows, err := s.DB.Platform.Query(ctx, `SELECT b.group_id::text
		FROM group_business_runtime r JOIN group_bindings b ON b.group_id=r.group_id AND b.tenant_id=r.tenant_id
		JOIN accounts a ON a.id=b.primary_account_id JOIN native_groups g ON g.id=b.group_id
		JOIN account_receivers cr ON cr.account_id=a.id
		WHERE r.enabled AND b.active AND b.management_enabled
		AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL AND a.purpose='management'
		AND cr.enabled AND cr.owner<>'' AND cr.lease_until>clock_timestamp()+interval '3 seconds'
		AND cr.observed_at>clock_timestamp()-interval '15 seconds' AND cr.observed_at<=clock_timestamp()
		AND cr.snapshot->>'state'='connected'
		AND a.platform=g.platform
		AND (b.config#>>'{feature_options,blocked_words,enabled}'='true'
			OR b.config#>>'{feature_options,blacklist,enabled}'='true'
			OR b.config#>>'{feature_options,keyword_replies,enabled}'='true')
		AND EXISTS(SELECT 1 FROM business_capability_gates c
			WHERE c.platform=g.platform AND c.source='automatic' AND (c.state='open' OR c.state='pilot'
			AND EXISTS(SELECT 1 FROM business_capability_pilots p WHERE p.platform=c.platform
				AND p.action=c.action AND p.source=c.source AND p.group_id=g.id AND p.gate_version=c.version)))
		ORDER BY b.group_id::text<=$1,b.group_id LIMIT $2`, after, businessGroupPage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, businessGroupPage)
	for rows.Next() {
		var group string
		if err = rows.Scan(&group); err != nil {
			return nil, err
		}
		out = append(out, group)
	}
	return out, rows.Err()
}

func (s *BusinessRulesService) messages(ctx context.Context, scope BusinessScope,
	conversation string, after, watermark int64, recovery bool) ([]BusinessMessage, error) {
	since := maxBusinessTime(scope.EnabledAt, time.Now().Add(-businessJobLifetime))
	rows, err := s.DB.Platform.Query(ctx, `SELECT e.id,e.received_at,e.event_key,d.payload_sealed,e.payload_sealed
		FROM native_received_events e JOIN native_decoded_events d ON d.event_id=e.id AND d.account_id=e.account_id
		WHERE e.account_id=$1 AND e.kind IN('group','private') AND d.scene='group' AND d.conversation_key=$2
		AND d.state IN('decoded','unsupported') AND e.payload_sealed IS NOT NULL
		AND e.id>$3 AND (NOT $5 OR e.id<=$4) AND e.received_at>=$6 AND e.received_at<=clock_timestamp()
		AND NOT EXISTS(SELECT 1 FROM business_rule_receipts r
			WHERE r.group_id=$7 AND r.account_id=e.account_id AND r.event_id=e.id)
		ORDER BY e.id LIMIT $8`, scope.Account, conversation, after, watermark, recovery,
		since, scope.Group, businessMessagePage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]BusinessMessage, 0, businessMessagePage)
	size := 0
	for rows.Next() {
		var id int64
		var at time.Time
		var key string
		var projection, source []byte
		if err = rows.Scan(&id, &at, &key, &projection, &source); err != nil {
			return nil, err
		}
		size += len(projection) + len(source)
		if size > 2<<20 && len(messages) > 0 {
			break
		}
		m, e := s.ReadSource(scope, key, id, projection, source)
		if e != nil {
			m = BusinessMessage{}
		}
		m.EventID, m.ReceivedAt = id, at
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

func (s *BusinessRulesService) discover(ctx context.Context, group string, recovery bool) error {
	if s.Conversation == nil || s.ReadSource == nil || s.FreshRole == nil || s.Vault == nil {
		return Denied(503, "business_runtime_unavailable")
	}
	scope, config, err := s.scope(ctx, s.DB.Platform, group)
	if err != nil {
		return err
	}
	var watermark, late int64
	var recoveryDue bool
	err = s.DB.Platform.QueryRow(ctx, `INSERT INTO business_rule_consumers
		(group_id,tenant_id,account_id,platform,binding_epoch,runtime_version,starts_at)
		VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(group_id) DO UPDATE SET
		tenant_id=excluded.tenant_id,account_id=excluded.account_id,platform=excluded.platform,
		source_watermark=CASE WHEN business_rule_consumers.account_id<>excluded.account_id
			OR business_rule_consumers.starts_at<>excluded.starts_at THEN 0 ELSE business_rule_consumers.source_watermark END,
		late_watermark=CASE WHEN business_rule_consumers.account_id<>excluded.account_id
			OR business_rule_consumers.starts_at<>excluded.starts_at THEN 0 ELSE business_rule_consumers.late_watermark END,
		last_recovery_at=CASE WHEN business_rule_consumers.account_id<>excluded.account_id
			OR business_rule_consumers.starts_at<>excluded.starts_at THEN NULL ELSE business_rule_consumers.last_recovery_at END,
		starts_at=excluded.starts_at,binding_epoch=excluded.binding_epoch,runtime_version=excluded.runtime_version,
		last_scan_at=clock_timestamp() RETURNING source_watermark,late_watermark,
		last_recovery_at IS NULL OR last_recovery_at<=clock_timestamp()-interval '5 seconds'`,
		scope.Group, scope.Tenant, scope.Account, scope.Platform, scope.BindingEpoch,
		scope.RuntimeVersion, scope.EnabledAt).Scan(&watermark, &late, &recoveryDue)
	if err != nil {
		return err
	}
	conversation, err := s.Conversation(ctx, scope)
	if err != nil {
		return err
	}
	messages, err := s.messages(ctx, scope, conversation, watermark, 0, false)
	if err != nil {
		return err
	}
	oldWatermark := watermark
	if err = s.consumeMessages(ctx, scope, config, messages); err != nil {
		return err
	}
	if len(messages) > 0 {
		watermark = messages[len(messages)-1].EventID
	}
	// Each enabled group owns its persisted recovery cadence. A global modulo
	// can permanently skip the same pages when 100/300 groups rotate.
	recovery = recovery || recoveryDue
	if recovery && oldWatermark > 0 {
		messages, err = s.messages(ctx, scope, conversation, late, oldWatermark, true)
		if err != nil {
			return err
		}
		if err = s.consumeMessages(ctx, scope, config, messages); err != nil {
			return err
		}
		if len(messages) > 0 {
			late = messages[len(messages)-1].EventID
		} else {
			late = 0
		}
	}
	_, err = s.DB.Platform.Exec(ctx, `UPDATE business_rule_consumers SET source_watermark=greatest(source_watermark,$2),
		late_watermark=$3,last_scan_at=clock_timestamp(),
		last_recovery_at=CASE WHEN $7 THEN clock_timestamp() ELSE last_recovery_at END
		WHERE group_id=$1 AND account_id=$4 AND starts_at=$5 AND runtime_version=$6`,
		scope.Group, watermark, late, scope.Account, scope.EnabledAt, scope.RuntimeVersion, recovery)
	return err
}

func (s *BusinessRulesService) consumeMessages(ctx context.Context, scope BusinessScope,
	config features.Config, messages []BusinessMessage) error {
	if len(messages) == 0 {
		return nil
	}
	rows, err := s.DB.Platform.Query(ctx, `SELECT native_uid FROM accounts
		WHERE platform=$1 AND native_uid IS NOT NULL LIMIT 20`, scope.Platform)
	if err != nil {
		return err
	}
	robots := make(map[string]bool, 20)
	for rows.Next() {
		var nativeID string
		if err = rows.Scan(&nativeID); err != nil {
			break
		}
		robots[nativeID] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	roleReads := businessRoleReads
	for _, message := range messages {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = s.candidate(ctx, scope, config, message, robots, &roleReads); err != nil {
			return err
		}
	}
	return nil
}

func (s *BusinessRulesService) candidate(ctx context.Context, scope BusinessScope, config features.Config,
	message BusinessMessage, robots map[string]bool, roleReads *int) error {
	s.seen.Add(1)
	message.Challenge = message.Challenge || businessChallengeText(message.Text)
	decision, code := "ignored", ""
	payload := businessPayload{Sender: message.Sender}
	input := features.BusinessRuleInput{Sender: message.Sender, Text: message.Text,
		PoolRobot: robots[message.Sender] || message.Sender == scope.NativeSelf, Challenge: message.Challenge}
	switch {
	case message.Challenge:
		decision = "challenge"
	case message.Group != scope.NativeGroup || message.ClientID == "":
	case message.ReceivedAt.Before(scope.EnabledAt) || !message.ReceivedAt.Add(businessJobLifetime).After(time.Now()):
		decision = "expired"
	case features.BusinessRuleCandidate(config, input):
		// Full queues are dropped before any upstream role work.
		full, err := s.queueFull(ctx, scope)
		if err != nil {
			return err
		}
		if full {
			decision, code = "limited", "business_queue_full"
			break
		}
		cooling, err := s.candidateCooling(ctx, scope, config, input)
		if err != nil {
			return err
		}
		if cooling {
			decision, code = "cooldown", "business_rule_cooling"
			break
		}
		if *roleReads == 0 {
			decision, code = "limited", "business_role_read_limit"
			break
		}
		*roleReads--
		evidence, err := s.FreshRole(ctx, scope, message.Sender)
		if err != nil || evidence.Platform != scope.Platform || evidence.Account != scope.Account ||
			evidence.Group != scope.NativeGroup || evidence.SelfID != scope.NativeSelf ||
			evidence.Target != message.Sender || evidence.AccountEpoch != scope.AccountEpoch ||
			evidence.AccountVersion != scope.AccountVersion || evidence.ObservedAt.After(time.Now()) ||
			evidence.ObservedAt.IsZero() || time.Since(evidence.ObservedAt) > 5*time.Second {
			decision, code = "role_unknown", "business_native_role_unknown"
			break
		}
		input.Role = evidence.TargetRole
		payload.Decision = features.EvaluateBusinessRules(config, input)
		payload.Reference = message.Reference
		if payload.Decision.Action == "" {
			break
		}
		decision = "matched"
		if err = validateBusinessEvidence(scope, payload, config, evidence, time.Now()); err != nil {
			decision, code = "blocked", businessErrorCode(err)
		} else if err = s.observeOwner(ctx, scope, evidence); err != nil {
			decision, code = "blocked", businessErrorCode(err)
		}
	}
	return s.recordCandidate(ctx, scope, message, payload, decision, code)
}

// Challenge syntax only excludes it from business consumers; it never proves
// an identity or authorizes a binding.
func businessChallengeText(text string) bool {
	for _, prefix := range []string{"群绑定 ", "群接入 "} {
		if token, ok := strings.CutPrefix(text, prefix); ok && onboardingToken(token) {
			return true
		}
	}
	return false
}

// No role is inferred here: a candidate is skipped only when every action
// possible for any verified role would be cooling already.
func (s *BusinessRulesService) candidateCooling(ctx context.Context, scope BusinessScope,
	config features.Config, input features.BusinessRuleInput) (bool, error) {
	var reply, mute, account bool
	err := s.DB.Platform.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM business_rule_consumers WHERE group_id=$1
			AND last_reply_at>clock_timestamp()-make_interval(secs=>$4)),
		EXISTS(SELECT 1 FROM business_rule_member_cooldowns WHERE group_id=$1
			AND account_id=$2 AND member_id=$3 AND until_at>clock_timestamp()),
		EXISTS(SELECT 1 FROM business_rule_account_cooldowns WHERE account_id=$2
			AND next_write_at>clock_timestamp())`,
		scope.Group, scope.Account, input.Sender, features.KeywordReplyCooldown(config)).Scan(&reply, &mute, &account)
	if err != nil || account {
		return account, err
	}
	possible := false
	for _, role := range []string{"member", "admin", "owner"} {
		input.Role = role
		action := features.EvaluateBusinessRules(config, input).Action
		if action == "" {
			continue
		}
		possible = true
		if action == features.BusinessRecall || action == features.BusinessReply && !reply ||
			action == features.BusinessMute && !mute {
			return false, nil
		}
	}
	return possible, nil
}

func (s *BusinessRulesService) queueFull(ctx context.Context, scope BusinessScope) (bool, error) {
	var full bool
	err := s.DB.Platform.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM business_rule_jobs WHERE status='queued')>=$1
		OR (SELECT count(*) FROM business_rule_jobs WHERE status='queued' AND tenant_id=$2)>=$3
		OR EXISTS(SELECT 1 FROM business_rule_jobs WHERE group_id=$4 AND status='queued')
		OR EXISTS(SELECT 1 FROM business_rule_jobs WHERE account_id=$5 AND status='unknown')`,
		businessWaiting, scope.Tenant, businessTenantWait, scope.Group, scope.Account).Scan(&full)
	return full, err
}

func (s *BusinessRulesService) recordCandidate(ctx context.Context, scope BusinessScope, message BusinessMessage,
	payload businessPayload, decision, code string) error {
	if decision != "matched" {
		_, err := s.DB.Platform.Exec(ctx, `INSERT INTO business_rule_receipts
			(group_id,tenant_id,account_id,platform,event_id,decision,rule,result_code)
			SELECT b.group_id,b.tenant_id,b.primary_account_id,a.platform,$5,$6,$7,$8
			FROM group_bindings b JOIN accounts a ON a.id=b.primary_account_id
			JOIN group_business_runtime r ON r.group_id=b.group_id AND r.tenant_id=b.tenant_id
			WHERE b.group_id=$1 AND b.tenant_id=$2 AND b.primary_account_id=$3
			AND b.active AND b.management_enabled AND a.platform=$4
			AND b.version=$9 AND b.epoch=$10 AND r.enabled AND r.version=$11
			AND a.epoch=$12 AND a.version=$13
			ON CONFLICT DO NOTHING`, scope.Group, scope.Tenant, scope.Account, scope.Platform,
			message.EventID, decision, payload.Decision.Rule, code,
			scope.BindingVersion, scope.BindingEpoch, scope.RuntimeVersion, scope.AccountEpoch, scope.AccountVersion)
		if err == nil && decision == "limited" {
			s.limited.Add(1)
		}
		return err
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798504)`); err != nil {
		return err
	}
	current, config, err := s.scope(ctx, tx, scope.Group)
	if err != nil || !bytes.Equal(current.snapshot(), scope.snapshot()) {
		return Denied(409, "business_scope_changed")
	}
	tag, err := tx.Exec(ctx, `INSERT INTO business_rule_receipts
		(group_id,tenant_id,account_id,platform,event_id,decision,rule,result_code)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
		scope.Group, scope.Tenant, scope.Account, scope.Platform, message.EventID, decision, payload.Decision.Rule, code)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	if decision == "matched" {
		if err = s.authorizeAction(ctx, tx, current, config, payload.Decision.Rule, payload.Decision.Action); err != nil {
			decision, code = "blocked", businessErrorCode(err)
		} else {
			var cooling bool
			err = tx.QueryRow(ctx, `SELECT
				($1='send_text' AND EXISTS(SELECT 1 FROM business_rule_consumers WHERE group_id=$2
					AND last_reply_at>clock_timestamp()-make_interval(secs=>$5)))
				OR ($1='mute_member' AND EXISTS(SELECT 1 FROM business_rule_member_cooldowns
					WHERE group_id=$2 AND account_id=$3 AND member_id=$4 AND until_at>clock_timestamp()))`,
				payload.Decision.Action, scope.Group, scope.Account, payload.Sender,
				features.KeywordReplyCooldown(config)).Scan(&cooling)
			if err != nil {
				return err
			}
			full, e := s.queueFullTx(ctx, tx, scope)
			if e != nil {
				return e
			}
			switch {
			case cooling:
				decision, code = "cooldown", "business_rule_cooling"
			case full:
				decision, code = "limited", "business_queue_full"
			default:
				id := NewID()
				raw, e := json.Marshal(payload)
				if e != nil {
					return e
				}
				sealed, e := s.Vault.Seal("business-rule-payload:"+id, raw)
				clear(raw)
				if e != nil {
					return e
				}
				snapshot := scope.snapshot()
				sealedScope, e := s.Vault.Seal("business-rule-scope:"+id, snapshot)
				if e != nil {
					return e
				}
				priority := 0
				if payload.Decision.Action == features.BusinessReply {
					priority = 1
				}
				_, err = tx.Exec(ctx, `INSERT INTO business_rule_jobs
					(id,platform,tenant_id,actor_id,account_id,group_id,event_id,rule,action,priority,
					scope_snapshot,scope_sealed,payload_sealed,expires_at)
					VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
					id, scope.Platform, scope.Tenant, scope.Actor, scope.Account, scope.Group, message.EventID,
					payload.Decision.Rule, payload.Decision.Action, priority, snapshot, sealedScope, sealed,
					message.ReceivedAt.Add(businessJobLifetime))
				if err != nil {
					return err
				}
				decision, code = "queued", ""
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE business_rule_receipts SET decision=$4,result_code=$5
			WHERE group_id=$1 AND account_id=$2 AND event_id=$3`,
			scope.Group, scope.Account, message.EventID, decision, code); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if decision == "queued" {
		s.queued.Add(1)
	} else if decision == "limited" {
		s.limited.Add(1)
	}
	return nil
}

func (s *BusinessRulesService) queueFullTx(ctx context.Context, tx pgx.Tx, scope BusinessScope) (bool, error) {
	var full bool
	err := tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM business_rule_jobs WHERE status='queued')>=$1
		OR (SELECT count(*) FROM business_rule_jobs WHERE tenant_id=$2 AND status='queued')>=$3
		OR EXISTS(SELECT 1 FROM business_rule_jobs WHERE group_id=$4 AND status='queued')
		OR EXISTS(SELECT 1 FROM business_rule_jobs WHERE account_id=$5 AND status='unknown')`,
		businessWaiting, scope.Tenant, businessTenantWait, scope.Group, scope.Account).Scan(&full)
	return full, err
}

func maxBusinessTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
