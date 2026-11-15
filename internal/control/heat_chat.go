package control

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/database"
	"wangshangliao/internal/heatbot"
	"wangshangliao/internal/scopecache"
	"wangshangliao/internal/security"
)

type HeatMessage struct {
	EventID                                      int64
	ReceivedAt                                   time.Time
	Group, Sender, Text, ClientID, ReplyClientID string
	Mentions                                     []string
}
type HeatChatService struct {
	DB                                 *database.DB
	Vault                              *security.Vault
	Gateway                            *GatewayService
	Cache                              *scopecache.Cache
	Conversation                       func(context.Context, HeatScope) (string, error)
	Decode                             func(HeatScope, string, int64, []byte, []byte) (HeatMessage, error)
	FreshRole                          func(context.Context, HeatScope) error
	Generate                           func(context.Context, heatbot.Request) (heatbot.Proposal, error)
	Send                               func(context.Context, HeatSend, nativeFence) (bool, error)
	BusinessRules                      *BusinessRulesService
	mu                                 sync.Mutex
	active                             map[string]bool
	candidates, models, sends, blocked atomic.Uint64
}

type nativeFence = func(context.Context, func(context.Context) error) error
type HeatRuntimeView struct {
	Platform  string                 `json:"platform,omitempty"`
	Robot     string                 `json:"robot_nickname,omitempty"`
	Effective *HeatEffectiveSettings `json:"effective_settings,omitempty"`
	State     string                 `json:"runtime_state"`
	Blocked   string                 `json:"blocked_reason,omitempty"`
	SampledAt time.Time              `json:"sampled_at"`
}

func (s *HeatChatService) View(ctx context.Context, group string) HeatRuntimeView {
	out := HeatRuntimeView{State: "blocked", SampledAt: time.Now().UTC()}
	scope, err := s.heatScope(ctx, s.DB.Platform, group, false)
	if err != nil {
		out.Blocked = "heat_chat_scope_unavailable"
		return out
	}
	out.Effective = &scope.Effective
	out.Platform = scope.Platform
	_ = s.DB.Platform.QueryRow(ctx, `SELECT native_nickname FROM accounts WHERE id=$1`, scope.Account).Scan(&out.Robot)
	var enabled, active bool
	if err = s.DB.Platform.QueryRow(ctx, `SELECT h.enabled,b.ai_enabled FROM heat_chat_admission h
		JOIN group_bindings b ON b.group_id=$1 JOIN native_groups g ON g.id=b.group_id
		WHERE h.platform=g.platform`, group).Scan(&enabled, &active); err != nil {
		out.Blocked = "heat_chat_status_unavailable"
		return out
	}
	var connected, member bool
	err = s.DB.Platform.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM account_receivers WHERE account_id=$1 AND enabled
			AND owner<>'' AND lease_until>clock_timestamp()
			AND observed_at>clock_timestamp()-interval '15 seconds' AND observed_at<=clock_timestamp()
			AND snapshot->>'state'='connected'),
		EXISTS(SELECT 1 FROM account_groups WHERE account_id=$1 AND group_id=$2
			AND role IN('member','admin','owner') AND observation_source='native'
			AND observed_at>clock_timestamp()-interval '120 seconds' AND observed_at<=clock_timestamp())`,
		scope.Account, scope.Group).Scan(&connected, &member)
	if err != nil {
		out.Blocked = "heat_chat_status_unavailable"
		return out
	}
	switch {
	case !enabled:
		out.Blocked = "heat_chat_platform_not_admitted"
	case !scope.Effective.Enabled:
		out.Blocked = "heat_chat_account_disabled"
	case !active:
		out.Blocked = "heat_chat_group_not_enabled"
	case !connected:
		out.Blocked = "heat_chat_receiver_unavailable"
	case !member:
		out.Blocked = "heat_chat_native_role_unavailable"
	case !scope.Effective.allows(time.Now()):
		out.State = "outside_hours"
	default:
		out.State = "running"
	}
	return out
}

func (s *HeatChatService) Recover(ctx context.Context) error {
	// A provider request or native write may already have happened. Neither is
	// replayed after a restart, even when the caller never received the result.
	_, err := s.DB.Platform.Exec(ctx, `UPDATE heat_chat_jobs SET
		status=CASE WHEN status IN('generating','sending') THEN 'unknown' ELSE 'expired' END,
		result_code='process_restarted',updated_at=clock_timestamp()
		WHERE status IN('generating','sending') OR status IN('queued','generated') AND expires_at<=clock_timestamp()`)
	return err
}

func (s *HeatChatService) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		var workers sync.WaitGroup
		defer workers.Wait()
		s.mu.Lock()
		s.active = map[string]bool{}
		s.mu.Unlock()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		var after string
		for ctx.Err() == nil {
			work, cancel := context.WithTimeout(ctx, 3*time.Second)
			groups, err := s.groups(work, after)
			cancel()
			if err == nil {
				for _, group := range groups {
					after = group
					work, cancel := context.WithTimeout(ctx, 3*time.Second)
					_ = s.discover(work, group)
					cancel()
				}
			}
			work, cancel = context.WithTimeout(ctx, 2*time.Second)
			ids, _ := s.dispatchable(work)
			cancel()
			for _, id := range ids {
				s.mu.Lock()
				if s.active[id] || len(s.active) >= 4 {
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
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}

func (s *HeatChatService) groups(ctx context.Context, after string) ([]string, error) {
	rows, err := s.DB.Platform.Query(ctx, `SELECT b.group_id::text FROM group_bindings b
		JOIN native_groups g ON g.id=b.group_id JOIN heat_chat_admission h ON h.platform=g.platform
		JOIN accounts a ON a.id=b.ai_account_id JOIN account_receivers cr ON cr.account_id=a.id
		WHERE b.active AND b.ai_requested AND b.ai_enabled AND h.enabled AND a.purpose='chat'
		AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
		AND cr.enabled AND cr.owner<>'' AND cr.lease_until>clock_timestamp()+interval '3 seconds'
		AND cr.observed_at>clock_timestamp()-interval '15 seconds' AND cr.observed_at<=clock_timestamp()
		AND cr.snapshot->>'state'='connected'
		ORDER BY b.group_id::text<=$1,b.group_id LIMIT 10`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *HeatChatService) messages(ctx context.Context, scope HeatScope, since time.Time, history bool) ([]HeatMessage, error) {
	if err := scope.controlledExecutionFence.require(ctx, s.DB.Platform, "", scope.Platform, scope.Group); err != nil {
		return nil, err
	}
	if s.Conversation == nil || s.Decode == nil {
		return nil, Denied(503, "heat_chat_source_unavailable")
	}
	conversation, err := s.Conversation(ctx, scope)
	if err != nil {
		return nil, err
	}
	order := "ASC"
	limit := 100
	if history {
		order = "DESC"
		limit = 40
	}
	rows, err := s.DB.Platform.Query(ctx, `SELECT e.id,e.received_at,e.event_key,d.payload_sealed,e.payload_sealed
		FROM native_decoded_events d JOIN native_received_events e ON e.id=d.event_id AND e.account_id=d.account_id
		JOIN group_bindings b ON b.group_id=$4 AND b.tenant_id=$7 AND b.ai_account_id=d.account_id
			AND b.active AND b.epoch=$8 AND b.version=$9
		JOIN native_groups g ON g.id=b.group_id AND g.platform=$10
		WHERE d.account_id=$1 AND d.scene='group' AND d.conversation_key=$2
		AND d.state IN('decoded','unsupported') AND e.received_at>=greatest($3::timestamptz,b.data_since)
		AND d.received_at>=b.data_since AND e.received_at<=clock_timestamp()
		AND e.payload_sealed IS NOT NULL AND ($5 OR NOT EXISTS(SELECT 1 FROM heat_chat_receipts r
			WHERE r.group_id=$4 AND r.account_id=e.account_id AND r.event_id=e.id))
		ORDER BY e.id `+order+` LIMIT $6`, scope.Account, conversation, since, scope.Group, history, limit,
		scope.Tenant, scope.BindingEpoch, scope.BindingVersion, scope.Platform)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HeatMessage{}
	total := 0
	for rows.Next() {
		var id int64
		var at time.Time
		var key string
		var projection, raw []byte
		if err = rows.Scan(&id, &at, &key, &projection, &raw); err != nil {
			return nil, err
		}
		total += len(projection) + len(raw)
		if total > 2<<20 {
			break
		}
		m, e := s.Decode(scope, key, id, projection, raw)
		m.EventID, m.ReceivedAt = id, at
		if e != nil {
			m = HeatMessage{EventID: id, ReceivedAt: at}
		}
		out = append(out, m)
	}
	if history {
		slices.Reverse(out)
	}
	if err = scope.controlledExecutionFence.require(ctx, s.DB.Platform, "", scope.Platform, scope.Group); err != nil {
		return nil, err
	}
	return out, rows.Err()
}

func (s *HeatChatService) discover(ctx context.Context, group string) error {
	scope, err := s.heatScope(ctx, s.DB.Platform, group, true)
	if err != nil {
		return err
	}
	if !scope.Effective.allows(time.Now()) {
		return nil
	}
	var starts time.Time
	err = s.DB.Platform.QueryRow(ctx, `INSERT INTO heat_chat_consumers(group_id,tenant_id,account_id,platform,binding_epoch)
		VALUES($1,$2,$3,$4,$5) ON CONFLICT(group_id) DO UPDATE SET
		tenant_id=excluded.tenant_id,account_id=excluded.account_id,platform=excluded.platform,
		starts_at=CASE WHEN heat_chat_consumers.account_id<>excluded.account_id
			OR heat_chat_consumers.binding_epoch<>excluded.binding_epoch THEN clock_timestamp() ELSE heat_chat_consumers.starts_at END,
		binding_epoch=excluded.binding_epoch,last_scan_at=clock_timestamp() RETURNING starts_at`,
		scope.Group, scope.Tenant, scope.Account, scope.Platform, scope.BindingEpoch).Scan(&starts)
	if err != nil {
		return err
	}
	since := time.Now().Add(-120 * time.Second)
	if starts.After(since) {
		since = starts
	}
	messages, err := s.messages(ctx, scope, since, false)
	if err != nil {
		return err
	}
	// Identity inventory is bounded by the two ten-account pools.
	rows, err := s.DB.Platform.Query(ctx, `SELECT native_uid FROM accounts WHERE platform=$1 AND native_uid IS NOT NULL LIMIT 20`, scope.Platform)
	if err != nil {
		return err
	}
	robots := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		robots[id] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, m := range messages {
		if err = s.candidate(ctx, scope, m, robots); err != nil {
			return err
		}
	}
	return nil
}

func (s *HeatChatService) candidate(ctx context.Context, scope HeatScope, m HeatMessage, robots map[string]bool) error {
	directed := slices.Contains(m.Mentions, scope.NativeSelf)
	if m.ReplyClientID != "" {
		var own bool
		err := s.DB.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM heat_chat_send_attempts a
			JOIN heat_chat_jobs j ON j.id=a.job_id WHERE j.group_id=$1 AND j.account_id=$2
			AND a.client_id::text=$3 AND j.status IN('sent','unknown'))`, scope.Group, scope.Account, m.ReplyClientID).Scan(&own)
		if err != nil {
			return err
		}
		directed = directed || own
	}
	decision := "queued"
	if m.Group != scope.NativeGroup || m.Text == "" || robots[m.Sender] || m.Sender == scope.NativeSelf {
		decision = "ignored"
	}
	if decision == "queued" && s.BusinessRules != nil {
		suppressed, err := s.BusinessRules.SuppressesHeat(ctx, scope, m)
		if err != nil {
			return err
		}
		if suppressed {
			decision = "ignored"
		}
	}
	var random [8]byte
	if decision == "queued" && !directed {
		if _, err := rand.Read(random[:]); err != nil {
			return err
		}
		if float64(binary.LittleEndian.Uint64(random[:])>>11)/(1<<53) >= scope.Effective.ReplyProbability {
			decision = "sampled_out"
		}
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798490)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT group_id FROM heat_chat_consumers WHERE group_id=$1 FOR UPDATE`, scope.Group); err != nil {
		return err
	}
	current, err := s.heatScope(ctx, tx, scope.Group, true)
	if err != nil || !bytes.Equal(current.digest(), scope.digest()) {
		return Denied(409, "heat_chat_scope_changed")
	}
	tag, err := tx.Exec(ctx, `INSERT INTO heat_chat_receipts(group_id,account_id,event_id,decision)VALUES($1,$2,$3,$4)
		ON CONFLICT DO NOTHING`, scope.Group, scope.Account, m.EventID, decision)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	s.candidates.Add(1)
	if decision != "queued" {
		return tx.Commit(ctx)
	}
	var cooling bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM heat_chat_consumers WHERE group_id=$1
		AND last_send_at>clock_timestamp()-$2::double precision*interval '1 second')`,
		scope.Group, scope.Effective.CooldownSeconds).Scan(&cooling); err != nil {
		return err
	}
	if cooling {
		_, err = tx.Exec(ctx, `UPDATE heat_chat_receipts SET decision='cooldown' WHERE group_id=$1 AND account_id=$2 AND event_id=$3`,
			scope.Group, scope.Account, m.EventID)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	var pendingDirected bool
	err = tx.QueryRow(ctx, `SELECT directed FROM heat_chat_jobs WHERE group_id=$1 AND status='queued' FOR UPDATE`, scope.Group).Scan(&pendingDirected)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == nil && pendingDirected && !directed {
		_, err = tx.Exec(ctx, `UPDATE heat_chat_receipts SET decision='ignored'
			WHERE group_id=$1 AND account_id=$2 AND event_id=$3`, scope.Group, scope.Account, m.EventID)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	var total, tenant int
	if err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE tenant_id=$1) FROM heat_chat_jobs WHERE status='queued'`, scope.Tenant).Scan(&total, &tenant); err != nil {
		return err
	}
	if total >= 64 || tenant >= 8 {
		_, err = tx.Exec(ctx, `UPDATE heat_chat_receipts SET decision='ignored'
			WHERE group_id=$1 AND account_id=$2 AND event_id=$3`, scope.Group, scope.Account, m.EventID)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE heat_chat_jobs SET status='superseded',result_code='candidate_superseded',
		updated_at=clock_timestamp() WHERE group_id=$1 AND status='queued'`, scope.Group); err != nil {
		return err
	}
	id := NewID()
	raw, _ := json.Marshal(scope)
	sealed, err := s.Vault.Seal("heat-chat-scope:"+id, raw)
	clear(raw)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO heat_chat_jobs(id,platform,tenant_id,actor_id,account_id,group_id,event_id,directed,
		scope_digest,scope_sealed,expires_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		id, scope.Platform, scope.Tenant, scope.Actor, scope.Account, scope.Group, m.EventID, directed, scope.digest(), sealed, m.ReceivedAt.Add(120*time.Second))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *HeatChatService) dispatchable(ctx context.Context) ([]string, error) {
	_, _ = s.DB.Platform.Exec(ctx, `UPDATE heat_chat_jobs SET status='expired',result_code='candidate_expired',
		updated_at=clock_timestamp() WHERE status IN('queued','generated') AND expires_at<=clock_timestamp()`)
	s.mu.Lock()
	active := make([]string, 0, len(s.active))
	for id := range s.active {
		active = append(active, id)
	}
	s.mu.Unlock()
	rows, err := s.DB.Platform.Query(ctx, `WITH eligible AS (
		SELECT j.*,row_number() OVER(PARTITION BY j.tenant_id ORDER BY j.directed DESC,j.updated_at,j.id) AS tenant_rank
		FROM heat_chat_jobs j
		WHERE j.status IN('queued','generated') AND j.expires_at>clock_timestamp()
		AND j.id::text<>ALL($1::text[])
		AND NOT EXISTS(SELECT 1 FROM heat_chat_jobs a WHERE a.group_id=j.group_id AND a.id<>j.id
			AND a.status IN('generating','generated','sending'))
		AND (j.status='generated' OR
			(SELECT count(*) FROM heat_chat_jobs a WHERE a.tenant_id=j.tenant_id AND a.status IN('generating','generated','sending'))<2)
		AND NOT EXISTS(SELECT 1 FROM heat_chat_account_cooldowns c WHERE c.account_id=j.account_id
			AND c.next_send_at>clock_timestamp())
		) SELECT id::text FROM eligible ORDER BY tenant_rank,directed DESC,updated_at,id LIMIT 4`, active)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *HeatChatService) execute(ctx context.Context, id string) (result error) {
	defer func() {
		if result != nil {
			work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			_, _ = s.DB.Platform.Exec(work, `UPDATE heat_chat_jobs SET status='unknown',
				result_code='heat_chat_work_unconfirmed',updated_at=clock_timestamp()
				WHERE id=$1 AND status IN('generating','sending')`, id)
		}
	}()
	if s.Generate == nil || s.Send == nil || s.FreshRole == nil {
		return Denied(503, "heat_chat_runtime_unavailable")
	}
	var group, status string
	var digest []byte
	var expiry time.Time
	err := s.DB.Platform.QueryRow(ctx, `SELECT group_id::text,status,scope_digest,expires_at FROM heat_chat_jobs
		WHERE id=$1 AND status IN('queued','generated') AND expires_at>clock_timestamp()`, id).Scan(&group, &status, &digest, &expiry)
	if err != nil {
		return err
	}
	scope, err := s.heatScope(ctx, s.DB.Platform, group, true)
	if err != nil || !bytes.Equal(scope.digest(), digest) {
		return s.finish(ctx, id, "cancelled", "heat_chat_scope_changed")
	}
	if !scope.Effective.allows(time.Now()) {
		return s.finish(ctx, id, "cancelled", "heat_chat_outside_hours")
	}
	if err = s.FreshRole(ctx, scope); err != nil {
		return s.finish(ctx, id, "failed", "heat_chat_native_role_unavailable")
	}
	// Native role refresh may invalidate the binding; never carry an old scope.
	current, err := s.heatScope(ctx, s.DB.Platform, group, true)
	if err != nil || !bytes.Equal(current.digest(), digest) {
		return s.finish(ctx, id, "cancelled", "heat_chat_scope_changed")
	}
	if status == "queued" {
		tx, e := s.DB.Platform.Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		if _, e = tx.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, scope.Tenant); e != nil {
			return e
		}
		var busy bool
		if e = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM heat_chat_jobs WHERE tenant_id=$1
			AND status IN('generating','generated','sending'))>=2 OR EXISTS(SELECT 1 FROM heat_chat_jobs
			WHERE group_id=$2 AND status IN('generating','generated','sending'))`, scope.Tenant, group).Scan(&busy); e != nil {
			return e
		}
		if busy {
			return nil
		}
		tag, e := tx.Exec(ctx, `UPDATE heat_chat_jobs SET status='generating',updated_at=clock_timestamp()
			WHERE id=$1 AND status='queued' AND expires_at>clock_timestamp()`, id)
		if e != nil || tag.RowsAffected() != 1 {
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		deadline := minHeatDeadline(expiry, time.Now().Add(90*time.Second))
		work, cancel := context.WithDeadline(ctx, deadline)
		defer cancel()
		work, stop, e := scope.controlledExecutionFence.modelContext(work, s.DB.Platform)
		if e != nil {
			return s.finish(ctx, id, "cancelled", heatErrorCode(e))
		}
		defer stop()
		deadline, _ = work.Deadline()
		messages, e := s.messages(work, scope, time.Now().Add(-24*time.Hour), true)
		if e != nil {
			return s.finish(ctx, id, "failed", "heat_chat_context_unavailable")
		}
		history := []heatbot.Message{}
		size := 0
		for i := len(messages) - 1; i >= 0; i-- {
			m := messages[i]
			if m.Group != scope.NativeGroup || m.Text == "" {
				continue
			}
			if size+len(m.Text) > 32768 {
				break
			}
			size += len(m.Text)
			role := "user"
			if m.Sender == scope.NativeSelf {
				role = "assistant"
			}
			history = append(history, heatbot.Message{Role: role, Content: m.Text})
		}
		slices.Reverse(history)
		if len(history) == 0 {
			return s.finish(ctx, id, "failed", "heat_chat_context_empty")
		}
		token, e := s.mintJobGrant(work, id, scope, deadline)
		if e != nil {
			return s.finish(ctx, id, "failed", "heat_chat_gateway_not_admitted")
		}
		watchDone := make(chan struct{})
		defer close(watchDone)
		go func() {
			timer := time.NewTicker(5 * time.Second)
			defer timer.Stop()
			for {
				select {
				case <-watchDone:
					return
				case <-work.Done():
					return
				case <-timer.C:
					check, stop := context.WithTimeout(work, time.Second)
					current, err := s.heatScope(check, s.DB.Platform, scope.Group, true)
					stop()
					if err != nil || !bytes.Equal(current.digest(), scope.digest()) || !current.Effective.allows(time.Now()) {
						cancel()
						return
					}
				}
			}
		}()
		persona := scope.Persona
		if scope.ReplyStyle != "" {
			persona += "\n\n" + scope.ReplyStyle
		}
		if len(persona) > 16000 {
			return s.finish(ctx, id, "failed", "heat_chat_persona_too_large")
		}
		proposal, e := s.Generate(work, heatbot.Request{ID: id, Scope: heatbot.Scope{Platform: scope.Platform, TenantID: scope.Tenant,
			GroupID: scope.Group, AccountID: scope.Account}, BindingVersion: scope.BindingVersion, AccountEpoch: scope.AccountEpoch,
			PersonaVersion: scope.PersonaVersion, Persona: persona, Messages: history, ExpiresAt: deadline,
			ModelAlias: "default", ModelVersion: scope.RouteVersion, GatewayToken: token,
			Settings: heatbot.Settings{Version: max(1, scope.SettingsVersion), MaxHistoryMessages: 40, MaxOutputTokens: 1024}})
		s.models.Add(1)
		if e != nil {
			return s.finish(ctx, id, "unknown", "heat_chat_generation_unconfirmed")
		}
		current, e = s.heatScope(work, s.DB.Platform, group, true)
		if e != nil || !bytes.Equal(current.digest(), scope.digest()) {
			return s.finish(ctx, id, "cancelled", "heat_chat_scope_changed")
		}
		if proposal.ID != id || proposal.Scope.Platform != scope.Platform || proposal.Scope.TenantID != scope.Tenant ||
			proposal.Scope.GroupID != scope.Group || proposal.Scope.AccountID != scope.Account ||
			proposal.BindingVersion != scope.BindingVersion || proposal.AccountEpoch != scope.AccountEpoch ||
			proposal.PersonaVersion != scope.PersonaVersion || len(proposal.Text) == 0 || len(proposal.Text) > 4096 {
			return s.finish(ctx, id, "failed", "heat_chat_proposal_invalid")
		}
		raw, _ := json.Marshal(proposal.Text)
		sealed, e := s.Vault.Seal("heat-chat-response:"+id, raw)
		clear(raw)
		if e != nil {
			return e
		}
		tag, e = s.DB.Platform.Exec(work, `UPDATE heat_chat_jobs SET status='generated',response_sealed=$2,updated_at=clock_timestamp()
			WHERE id=$1 AND status='generating' AND expires_at>clock_timestamp()`, id, sealed)
		if e != nil || tag.RowsAffected() != 1 {
			return e
		}
	}
	return s.send(ctx, id, scope)
}

func (s *HeatChatService) finish(ctx context.Context, id, status, code string) error {
	if status != "sent" {
		s.blocked.Add(1)
	}
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_, err := s.DB.Platform.Exec(work, `UPDATE heat_chat_jobs SET status=$2,result_code=$3,updated_at=clock_timestamp()
		WHERE id=$1 AND status IN('queued','generating','generated','sending')`, id, status, code)
	return err
}

type HeatChatStats struct {
	Candidates uint64 `json:"candidates"`
	Models     uint64 `json:"model_calls"`
	Sends      uint64 `json:"confirmed_sends"`
	Blocked    uint64 `json:"blocked"`
	Active     int    `json:"active"`
}

func (s *HeatChatService) Snapshot() HeatChatStats {
	s.mu.Lock()
	active := len(s.active)
	s.mu.Unlock()
	return HeatChatStats{Candidates: s.candidates.Load(), Models: s.models.Load(), Sends: s.sends.Load(),
		Blocked: s.blocked.Load(), Active: active}
}
func minHeatDeadline(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *HeatChatService) mintJobGrant(ctx context.Context, id string, scope HeatScope, deadline time.Time) (string, error) {
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	current, err := s.heatScope(ctx, tx, scope.Group, true)
	if err != nil || !bytes.Equal(current.digest(), scope.digest()) {
		return "", Denied(409, "heat_chat_scope_changed")
	}
	token, err := security.Token()
	if err != nil {
		return "", err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO heat_gateway_grants(token_hash,tenant_id,actor_id,group_id,account_id,
		binding_version,binding_epoch,account_epoch,persona_version,model_alias,route_version,expires_at,chat_job_id,generation_deadline)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,'default',$10,clock_timestamp()+interval '30 seconds',$11,$12
		WHERE EXISTS(SELECT 1 FROM heat_chat_jobs WHERE id=$11 AND status='generating' AND scope_digest=$13)
		ON CONFLICT(chat_job_id) WHERE chat_job_id IS NOT NULL DO NOTHING`,
		security.Digest(token), scope.Tenant, scope.Actor, scope.Group, scope.Account, scope.BindingVersion, scope.BindingEpoch,
		scope.AccountEpoch, scope.PersonaVersion, scope.RouteVersion, id, deadline, scope.digest())
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() != 1 {
		return "", Denied(409, "heat_chat_generation_already_recorded")
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return token, nil
}

func (s *HeatChatService) CheckJob(ctx context.Context, id string) error {
	var group string
	var digest []byte
	err := s.DB.Platform.QueryRow(ctx, `SELECT j.group_id::text,j.scope_digest FROM heat_chat_jobs j
		JOIN heat_gateway_grants h ON h.chat_job_id=j.id
		WHERE j.id=$1 AND j.status='generating' AND j.expires_at>clock_timestamp()
		AND h.used_at IS NOT NULL AND h.generation_deadline>clock_timestamp()`, id).Scan(&group, &digest)
	if err != nil {
		return Denied(409, "heat_chat_job_changed")
	}
	scope, err := s.heatScope(ctx, s.DB.Platform, group, true)
	if err != nil || !bytes.Equal(digest, scope.digest()) || !scope.Effective.allows(time.Now()) {
		return Denied(409, "heat_chat_scope_changed")
	}
	var allowed bool
	err = s.DB.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account_groups WHERE account_id=$1
		AND group_id=$2 AND role IN('member','admin','owner') AND observation_source='native'
		AND observed_at<=clock_timestamp() AND observed_at>clock_timestamp()-interval '120 seconds')`,
		scope.Account, scope.Group).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return Denied(409, "heat_chat_native_role_unavailable")
	}
	return nil
}

func (s *HeatChatService) Prune(ctx context.Context) error {
	_, err := s.DB.Platform.Exec(ctx, `DELETE FROM heat_chat_receipts WHERE (group_id,account_id,event_id) IN
		(SELECT group_id,account_id,event_id FROM heat_chat_receipts WHERE created_at<clock_timestamp()-interval '7 days' LIMIT 512);
		UPDATE heat_chat_jobs SET response_sealed=NULL WHERE id IN
			(SELECT id FROM heat_chat_jobs WHERE created_at<clock_timestamp()-interval '7 days'
				AND response_sealed IS NOT NULL LIMIT 512)`)
	return err
}

func heatErrorCode(err error) string {
	var f *Failure
	if errors.As(err, &f) {
		return f.Code
	}
	return fmt.Sprintf("heat_chat_%s", "unavailable")
}
