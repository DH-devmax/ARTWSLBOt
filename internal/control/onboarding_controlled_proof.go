package control

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

type OnboardingControlledProofInput struct {
	Version int64  `json:"version"`
	Kind    string `json:"kind"`
	Text    string `json:"text"`
	Seconds int    `json:"seconds"`
	Key     string `json:"-"`
}

type OnboardingControlledProofWindow struct {
	ID string `json:"id"`
	TestScope
	AccountID        string `json:"account_id"`
	ProofKind        string `json:"proof_kind"`
	ChallengeVersion int64  `json:"challenge_version"`
}

type OnboardingControlledProofChoice struct {
	OnboardingControlledProofWindow
	GroupName string `json:"group_name"`
}

type controlledProofState struct {
	Challenge, Actor, Tenant, Platform, Scope, Group, NativeGroup string
	Account, Sender, Receiver, Recipient                          string
	ChallengeVersion, ScopeVersion, Epoch, AccountVersion         int64
	ReceiverEpoch, ReceiverVersion, BindingEpoch, BindingVersion  int64
	ExpiresAt, ScopeExpiresAt                                     time.Time
	TokenHash, Identity, OwnerHash                                []byte
	Role                                                          string
	Binding                                                       onboardingControlledBinding
}

const controlledProofPrefix = "onboarding_proof:"

func controlledProofKind(kind string) bool { return kind == "private" || kind == "group" }

func controlledProofInputValid(id string, in OnboardingControlledProofInput) bool {
	prefix := "群绑定 "
	if in.Kind == "private" {
		prefix = "群接入 "
	}
	token, ok := strings.CutPrefix(in.Text, prefix)
	return ValidID(id) && in.Version > 0 && controlledProofKind(in.Kind) &&
		in.Seconds >= 5 && in.Seconds <= 300 && onboardingKey(in.Key) && ok && onboardingToken(token)
}

func controlledProofMarker(challenge, kind string) string {
	return controlledProofPrefix + challenge + ":" + kind
}

func controlledProofDigest(raw []byte) string {
	return hex.EncodeToString(security.Digest(string(raw)))
}

func (p controlledProofState) target(kind string) string {
	if kind == "private" {
		return p.Recipient
	}
	return p.NativeGroup
}

func (p controlledProofState) targets(kind, textDigest, requestDigest string) []string {
	return []string{p.target(kind), controlledProofMarker(p.Challenge, kind),
		"onboarding_proof_version:" + strconv.FormatInt(p.ChallengeVersion, 10),
		"onboarding_proof_scope:" + p.Scope + ":" + strconv.FormatInt(p.ScopeVersion, 10),
		"onboarding_proof_binding:" + p.Binding.marker(),
		"onboarding_proof_text:" + textDigest, "onboarding_proof_token:" + hex.EncodeToString(p.TokenHash),
		"onboarding_proof_identity:" + controlledProofDigest(p.Identity), "onboarding_proof_request:" + requestDigest}
}

// Prior controlled ownership and an unowned isolated fixture both require new
// authenticated private/group evidence. A grant never establishes ownership.
func loadControlledProofState(ctx context.Context, q Queryer, actor, platform, challenge, kind string, freshRole bool) (controlledProofState, error) {
	var p controlledProofState
	if !ValidID(actor) || !ValidID(challenge) || !bindingPlatform(platform) || !controlledProofKind(kind) {
		return p, Denied(409, "onboarding_controlled_proof_window_invalid")
	}
	err := q.QueryRow(ctx, `SELECT c.id::text,c.applicant_id::text,c.tenant_id::text,c.platform,s.id::text,
		s.group_id::text,g.native_id,a.id::text,a.native_uid,r.id::text,r.native_uid,
		c.version,s.version,a.epoch,a.version,r.epoch,r.version,b.epoch,b.version,c.expires_at,s.expires_at,
		CASE WHEN $4='private' THEN c.private_token_hash ELSE c.token_hash END,
		c.identity_sealed,s.owner_identity_hash,s.claimant_role
		FROM group_binding_challenges c JOIN onboarding_test_scopes s ON s.id=c.controlled_scope_id
		JOIN users u ON u.id=s.actor_id JOIN tenants t ON t.id=s.tenant_id
		JOIN accounts a ON a.id=s.claimant_account_id JOIN accounts r ON r.id=s.account_id
		JOIN native_groups g ON g.id=s.group_id JOIN group_bindings b ON b.group_id=g.id
		JOIN account_receivers ar ON ar.account_id=a.id JOIN account_receivers rr ON rr.account_id=r.id
		WHERE c.id=$1 AND c.applicant_id=$2 AND c.platform=$3 AND c.applicant_mode='operator_demo'
		AND c.friend_flow='incoming_contact' AND c.state='waiting' AND c.expires_at>clock_timestamp()+interval '3 seconds'
		AND c.tenant_id=s.tenant_id AND c.applicant_id=s.actor_id AND c.platform=s.platform
		AND c.account_id=s.account_id AND c.account_epoch=s.account_epoch AND c.account_version=s.account_version
		AND c.applicant_version=s.actor_version AND c.group_id=s.group_id AND onboarding_test_scope_active(s.id)
		AND u.role='superadmin' AND NOT u.disabled AND u.access_version=s.actor_version AND workspace_is_demo(t.id,u.id)
		AND a.id<>r.id AND a.native_uid<>r.native_uid AND a.native_uid=s.claimant_native_id
		AND a.platform=$3 AND r.platform=$3 AND a.workspace=$3 AND r.workspace=$3
		AND a.purpose='management' AND r.purpose='management'
		AND a.auth_state='authenticated' AND r.auth_state='authenticated'
		AND a.testing_enabled AND r.testing_enabled AND a.connection_enabled AND r.connection_enabled
		AND a.credential_sealed IS NOT NULL AND r.credential_sealed IS NOT NULL
		AND a.epoch=s.claimant_epoch AND a.version=s.claimant_version
		AND r.epoch=s.account_epoch AND r.version=s.account_version
		AND g.platform=$3 AND g.isolated_test AND b.active
		AND ar.enabled AND rr.enabled AND ar.owner<>'' AND rr.owner<>''
		AND ar.lease_until>clock_timestamp()+interval '3 seconds' AND rr.lease_until>clock_timestamp()+interval '3 seconds'
		AND ar.observed_at>clock_timestamp()-interval '15 seconds' AND ar.observed_at<=clock_timestamp()
		AND rr.observed_at>clock_timestamp()-interval '15 seconds' AND rr.observed_at<=clock_timestamp()
		AND ar.snapshot->>'state'='connected' AND rr.snapshot->>'state'='connected'
		AND ($4='private' AND c.identity_sealed IS NULL
			OR $4='group' AND c.identity_sealed IS NOT NULL AND c.identity_event_id IS NOT NULL)
		FOR SHARE OF c,s,u,t,a,r,g,b,ar,rr`, challenge, actor, platform, kind).
		Scan(&p.Challenge, &p.Actor, &p.Tenant, &p.Platform, &p.Scope, &p.Group, &p.NativeGroup,
			&p.Account, &p.Sender, &p.Receiver, &p.Recipient, &p.ChallengeVersion, &p.ScopeVersion,
			&p.Epoch, &p.AccountVersion, &p.ReceiverEpoch, &p.ReceiverVersion, &p.BindingEpoch, &p.BindingVersion,
			&p.ExpiresAt, &p.ScopeExpiresAt, &p.TokenHash, &p.Identity, &p.OwnerHash, &p.Role)
	if err == pgx.ErrNoRows {
		return p, Denied(409, "controlled_onboarding_scope_inactive")
	}
	if err != nil {
		return p, err
	}
	p.Binding, err = loadOnboardingControlledBinding(ctx, q, p.Group, p.Tenant, p.Account, p.Receiver, p.Platform)
	if err != nil {
		return p, err
	}
	if p.Binding.Epoch != p.BindingEpoch || p.Binding.Version != p.BindingVersion {
		return p, Denied(409, "controlled_onboarding_scope_changed")
	}
	if freshRole {
		var role string
		err = q.QueryRow(ctx, `SELECT role FROM account_groups WHERE account_id=$1 AND group_id=$2
			AND observation_source='native' AND role IN ('owner','admin')
			AND observed_at>clock_timestamp()-interval '57 seconds' AND observed_at<=clock_timestamp()
			FOR SHARE`, p.Account, p.Group).Scan(&role)
		if err == pgx.ErrNoRows || err == nil && p.Role != "" && p.Role != role {
			return p, ErrFreshRole
		}
	}
	return p, err
}

func (p controlledProofState) checkText(vault *security.Vault, kind, text string) error {
	prefix := "群绑定 "
	if kind == "private" {
		prefix = "群接入 "
	}
	if !bindingTokenMatches(p.TokenHash, text, prefix) {
		return Denied(409, "onboarding_controlled_proof_text_changed")
	}
	if kind == "group" {
		if vault == nil {
			return Denied(503, "group_binding_native_unavailable")
		}
		plain, err := vault.Open("group-binding-identity:"+p.Challenge, p.Identity)
		if err != nil {
			return Denied(409, "group_binding_identity_changed")
		}
		same := string(plain) == p.Sender
		clear(plain)
		if !same {
			return Denied(409, "group_binding_identity_changed")
		}
	}
	return nil
}

type controlledProofWindow struct {
	ID, Actor, Platform, Group, Admission string
	Accounts, Targets, Actions            []string
	Epochs                                map[string]int64
	Budget, Used                          int
	Cancelled                             bool
	CreatedAt, ExpiresAt                  time.Time
}

func readControlledProofWindow(ctx context.Context, q Queryer, id string) (controlledProofWindow, error) {
	var w controlledProofWindow
	var epochs []byte
	err := q.QueryRow(ctx, `SELECT w.id::text,w.actor_id::text,g.platform,w.group_id::text,w.admission_mode,
		w.account_ids::text[],w.target_ids,w.actions,w.account_epochs,w.budget,w.used,w.cancelled,w.created_at,w.expires_at
		FROM test_windows w JOIN native_groups g ON g.id=w.group_id WHERE w.id=$1 FOR UPDATE OF w FOR SHARE OF g`, id).
		Scan(&w.ID, &w.Actor, &w.Platform, &w.Group, &w.Admission, &w.Accounts, &w.Targets, &w.Actions,
			&epochs, &w.Budget, &w.Used, &w.Cancelled, &w.CreatedAt, &w.ExpiresAt)
	if err == pgx.ErrNoRows {
		return w, Denied(409, "onboarding_controlled_proof_window_invalid")
	}
	if err != nil {
		return w, err
	}
	if json.Unmarshal(epochs, &w.Epochs) != nil {
		return w, Denied(409, "onboarding_controlled_proof_window_invalid")
	}
	return w, nil
}

func controlledProofSelector(targets []string) (challenge, kind string, found bool, err error) {
	for _, target := range targets {
		if !strings.HasPrefix(target, controlledProofPrefix) {
			continue
		}
		if found {
			return "", "", true, Denied(409, "onboarding_controlled_proof_window_invalid")
		}
		parts := strings.Split(strings.TrimPrefix(target, controlledProofPrefix), ":")
		if len(parts) != 2 || !ValidID(parts[0]) || !controlledProofKind(parts[1]) {
			return "", "", true, Denied(409, "onboarding_controlled_proof_window_invalid")
		}
		challenge, kind, found = parts[0], parts[1], true
	}
	return
}

func (w controlledProofWindow) verify(p controlledProofState, kind string, reserved bool) (controlledExecutionFence, error) {
	var f controlledExecutionFence
	action := "send_text"
	if kind == "private" {
		action = "send_private_text"
	}
	if !ValidID(w.ID) || !controlledProofKind(kind) || len(w.Targets) != 9 ||
		!strings.HasPrefix(w.Targets[5], "onboarding_proof_text:") ||
		!strings.HasPrefix(w.Targets[8], "onboarding_proof_request:") {
		return f, Denied(409, "onboarding_controlled_proof_window_invalid")
	}
	textDigest := strings.TrimPrefix(w.Targets[5], "onboarding_proof_text:")
	requestDigest := strings.TrimPrefix(w.Targets[8], "onboarding_proof_request:")
	textHash, e1 := hex.DecodeString(textDigest)
	requestHash, e2 := hex.DecodeString(requestDigest)
	if e1 != nil || e2 != nil || len(textHash) != 32 || len(requestHash) != 32 ||
		w.Actor != p.Actor || w.Platform != p.Platform || w.Group != p.Group ||
		w.Admission != "native_debug" || !slices.Equal(w.Accounts, []string{p.Account}) ||
		!slices.Equal(w.Actions, []string{action}) || w.Budget != 1 || w.Used < 0 ||
		!reserved && w.Used != 0 || reserved && w.Used != 1 || w.Cancelled ||
		w.ExpiresAt.After(w.CreatedAt.Add(300*time.Second)) ||
		!w.ExpiresAt.After(time.Now().Add(3*time.Second)) ||
		w.ExpiresAt.After(p.ExpiresAt) || w.ExpiresAt.After(p.ScopeExpiresAt) ||
		len(w.Epochs) != 2 || w.Epochs[p.Account] != p.Epoch || w.Epochs[p.Receiver] != p.ReceiverEpoch ||
		!slices.Equal(w.Targets, p.targets(kind, textDigest, requestDigest)) {
		return f, Denied(409, "onboarding_controlled_proof_window_invalid")
	}
	f = controlledExecutionFence{ControlledScopeID: p.Scope, ControlledScopeVersion: p.ScopeVersion,
		ProofWindowID: w.ID, ProofChallengeID: p.Challenge, ProofChallengeVersion: p.ChallengeVersion,
		ProofKind: kind, ProofTextDigest: textDigest, ProofTarget: p.target(kind), ProofAccountID: p.Account}
	return f, nil
}

// Creation prepares a single existing-ledger send grant; it never sends a
// message, records a proof or consumes either of the shared native attempts.
func (s *GroupBindingService) CreateControlledProofWindow(ctx context.Context, ref SessionRef, id string,
	in OnboardingControlledProofInput) (OnboardingControlledProofWindow, error) {
	var out OnboardingControlledProofWindow
	if !controlledProofInputValid(id, in) {
		return out, Denied(400, "onboarding_controlled_proof_invalid")
	}
	if s.Native == nil || s.Vault == nil || s.DB == nil {
		return out, Denied(503, "group_binding_native_unavailable")
	}
	plain, _ := json.Marshal(struct {
		Input OnboardingControlledProofInput
		Key   string
	}{in, in.Key})
	requestDigest, textDigest := controlledProofDigest(plain), controlledProofDigest([]byte(in.Text))
	clear(plain)
	prepare := func() (pgx.Tx, controlledProofState, error) {
		var p controlledProofState
		tx, err := s.DB.Platform.Begin(ctx)
		if err != nil {
			return nil, p, err
		}
		_, err = tx.Exec(ctx, `SET LOCAL lock_timeout='750ms'`)
		var a Actor
		if err == nil {
			a, err = s.controlledOwner(ctx, tx, ref)
		}
		if err == nil {
			// Serialize only this challenge/kind. SHARE locks below fence its
			// versions without inverting the send path's window/challenge locks.
			_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,798569))`,
				controlledProofMarker(id, in.Kind))
		}
		if err == nil {
			p, err = loadControlledProofState(ctx, tx, a.ID, ref.assistantPlatform(), id, in.Kind, false)
		}
		if err == nil && p.ChallengeVersion != in.Version {
			err = Denied(409, "group_binding_version_conflict")
		}
		if err == nil {
			err = p.checkText(s.Vault, in.Kind, in.Text)
		}
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return nil, p, err
		}
		return tx, p, nil
	}
	reuse := func(tx pgx.Tx, p controlledProofState) (bool, error) {
		var previous string
		err := tx.QueryRow(ctx, `SELECT id::text FROM test_windows WHERE $1=ANY(target_ids) ORDER BY created_at,id LIMIT 1`,
			controlledProofMarker(id, in.Kind)).Scan(&previous)
		if err == pgx.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return true, err
		}
		w, err := readControlledProofWindow(ctx, tx, previous)
		if err != nil {
			return true, err
		}
		if !slices.Equal(w.Targets, p.targets(in.Kind, textDigest, requestDigest)) {
			return true, Denied(409, "onboarding_controlled_proof_window_exists")
		}
		if _, err = w.verify(p, in.Kind, w.Used == 1); err != nil {
			return true, err
		}
		out = p.view(w, in.Kind)
		return true, nil
	}
	tx, initial, err := prepare()
	if err != nil {
		return out, err
	}
	if found, err := reuse(tx, initial); found || err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return out, err
	}
	_ = tx.Rollback(context.WithoutCancel(ctx))
	source := bindingRecord{Account: initial.Account, Platform: initial.Platform, Epoch: initial.Epoch, AccountVersion: initial.AccountVersion}
	read, cancel := context.WithTimeout(ctx, 8*time.Second)
	roles, err := s.Native.BindingRoles(read, source.scope(initial.NativeGroup, initial.Sender))
	cancel()
	if err != nil {
		return out, err
	}
	if !roles.Present || roles.NativeGroupID != initial.NativeGroup || roles.NativeSenderID != initial.Sender ||
		!onboardingLabel(roles.NativeOwnerID, 200) || roles.RobotRole != roles.SenderRole ||
		roles.SenderRole != "owner" && roles.SenderRole != "admin" ||
		initial.Role != "" && roles.SenderRole != initial.Role ||
		len(initial.OwnerHash) != 0 && !bytes.Equal(initial.OwnerHash, bindingIdentityDigest(initial.Platform, initial.Group, roles.NativeOwnerID)) {
		return out, ErrFreshRole
	}
	tx, current, err := prepare()
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if !slices.Equal(initial.targets(in.Kind, textDigest, requestDigest), current.targets(in.Kind, textDigest, requestDigest)) {
		return out, Denied(409, "controlled_onboarding_scope_changed")
	}
	if found, err := reuse(tx, current); found || err != nil {
		return out, err
	}
	if _, err = s.connection(ctx, tx, source, &roles.GroupBindingConnectionEvidence); err != nil {
		return out, err
	}
	if _, err = applyNativeRoleTx(ctx, tx, current.Account, current.Group, roles.SenderRole, roles.ObservedAt); err != nil {
		return out, err
	}
	// Persisting fresh role evidence can advance a binding version. Never
	// prepare a window from the snapshot preceding that update.
	current, err = loadControlledProofState(ctx, tx, current.Actor, current.Platform, id, in.Kind, true)
	if err != nil {
		return out, err
	}
	epochs, _ := json.Marshal(map[string]int64{current.Account: current.Epoch, current.Receiver: current.ReceiverEpoch})
	w := controlledProofWindow{ID: NewID(), Actor: current.Actor, Platform: current.Platform, Group: current.Group,
		Admission: "native_debug", Accounts: []string{current.Account}, Targets: current.targets(in.Kind, textDigest, requestDigest),
		Actions: []string{"send_text"}, Budget: 1}
	if in.Kind == "private" {
		w.Actions = []string{"send_private_text"}
	}
	err = tx.QueryRow(ctx, `INSERT INTO test_windows(id,actor_id,group_id,account_ids,target_ids,actions,account_epochs,
		expires_at,budget,admission_mode) VALUES($1,$2,$3,ARRAY[$4::uuid],$5,$6,$7,
		least($8,$9,now()+make_interval(secs=>$10)),1,'native_debug') RETURNING created_at,expires_at`,
		w.ID, w.Actor, w.Group, current.Account, w.Targets, w.Actions, epochs, current.ExpiresAt, current.ScopeExpiresAt, in.Seconds).
		Scan(&w.CreatedAt, &w.ExpiresAt)
	if err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, Actor{ID: current.Actor}, current.Tenant, "onboarding_controlled_proof_window_created", w.ID,
		map[string]any{"challenge_id": id, "group_id": current.Group, "platform": current.Platform,
			"proof_kind": in.Kind, "account_id": current.Account, "budget": 1}); err != nil {
		return out, err
	}
	out = current.view(w, in.Kind)
	return out, tx.Commit(ctx)
}

func (p controlledProofState) view(w controlledProofWindow, kind string) OnboardingControlledProofWindow {
	return OnboardingControlledProofWindow{ID: w.ID, AccountID: p.Account, ProofKind: kind, ChallengeVersion: p.ChallengeVersion,
		TestScope: TestScope{GroupID: p.Group, NativeGroup: p.NativeGroup, AccountEpoch: p.Epoch,
			BindingEpoch: p.BindingEpoch, ExpiresAt: w.ExpiresAt, Remaining: w.Budget - w.Used, AdmissionMode: "native_debug"}}
}

// Messages may discover only exact, already prepared applicant proof windows.
// Discovery neither grants a destination nor replaces final send authorization.
func (s *NativeTextService) OnboardingControlledProofWindows(ctx context.Context, ref SessionRef,
	account, kind, target string) ([]OnboardingControlledProofChoice, error) {
	out := []OnboardingControlledProofChoice{}
	if !ValidID(account) || !controlledProofKind(kind) || !onboardingLabel(target, 200) {
		return out, Denied(400, "onboarding_controlled_proof_invalid")
	}
	if s.DB == nil {
		return out, Denied(503, "group_binding_native_unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='750ms'`); err != nil {
		return out, err
	}
	actor, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return out, err
	}
	if actor.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	action := "send_text"
	if kind == "private" {
		action = "send_private_text"
	}
	rows, err := tx.Query(ctx, `SELECT w.id::text,w.used,w.expires_at,g.name FROM test_windows w
		JOIN native_groups g ON g.id=w.group_id
		WHERE w.actor_id=$1 AND w.account_ids=ARRAY[$2::uuid] AND g.platform=$3
		AND w.actions=ARRAY[$4]::text[] AND $5=ANY(w.target_ids)
		AND w.admission_mode='native_debug' AND w.budget=1 AND w.used IN (0,1) AND NOT w.cancelled
		AND w.expires_at>clock_timestamp()+interval '3 seconds'
		AND EXISTS(SELECT 1 FROM unnest(w.target_ids) AS t(value) WHERE starts_with(t.value,'onboarding_proof:'))
		ORDER BY w.created_at DESC,w.id LIMIT 10`, actor.ID, account, ref.assistantPlatform(), action, target)
	if err != nil {
		return out, err
	}
	type candidate struct {
		id, name  string
		used      int
		expiresAt time.Time
	}
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.used, &c.expiresAt, &c.name); err != nil {
			rows.Close()
			return out, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, c := range candidates {
		fence, state, found, err := controlledProofExecution(ctx, tx, c.id, c.used == 1)
		if err != nil {
			var rejected *Failure
			if errors.As(err, &rejected) && rejected.Status == 409 {
				continue
			}
			return out, err
		}
		if !found || state.Actor != actor.ID || state.Account != account ||
			state.Platform != ref.assistantPlatform() || fence.ProofKind != kind || fence.ProofTarget != target {
			continue
		}
		w := controlledProofWindow{ID: c.id, Budget: 1, Used: c.used, ExpiresAt: c.expiresAt}
		out = append(out, OnboardingControlledProofChoice{OnboardingControlledProofWindow: state.view(w, kind), GroupName: c.name})
	}
	return out, tx.Commit(ctx)
}

func controlledProofExecution(ctx context.Context, q Queryer, window string, reserved bool) (controlledExecutionFence, controlledProofState, bool, error) {
	var p controlledProofState
	var f controlledExecutionFence
	w, err := readControlledProofWindow(ctx, q, window)
	if err != nil {
		return f, p, false, err
	}
	challenge, kind, found, err := controlledProofSelector(w.Targets)
	if !found || err != nil {
		return f, p, found, err
	}
	p, err = loadControlledProofState(ctx, q, w.Actor, w.Platform, challenge, kind, true)
	if err == nil {
		f, err = w.verify(p, kind, reserved)
	}
	if err == nil {
		var count int
		var prepared bool
		err = q.QueryRow(ctx, `SELECT count(*)::int,EXISTS(SELECT 1 FROM audit_events e
			WHERE e.action='onboarding_controlled_proof_window_created' AND e.resource_id=$2
			AND e.actor_id=$3 AND e.tenant_id=$4 AND e.detail->>'challenge_id'=$5
			AND e.detail->>'proof_kind'=$6 AND e.detail->>'account_id'=$7)
			FROM test_windows WHERE $1=ANY(target_ids)`,
			controlledProofMarker(challenge, kind), w.ID, p.Actor, p.Tenant, p.Challenge, kind, p.Account).
			Scan(&count, &prepared)
		if err == nil && (count != 1 || !prepared) {
			err = Denied(409, "onboarding_controlled_proof_window_invalid")
		}
	}
	return f, p, true, err
}

func controlledProofTextScope(ctx context.Context, q Queryer, vault *security.Vault, actor, platform string,
	req TextRequest, reserved bool, targets []string) (controlledExecutionFence, bool, error) {
	var empty controlledExecutionFence
	_, _, found, err := controlledProofSelector(targets)
	if !found || err != nil {
		return empty, found, err
	}
	f, p, _, err := controlledProofExecution(ctx, q, req.TestWindowID, reserved)
	if err != nil {
		return empty, true, err
	}
	if actor != p.Actor || platform != p.Platform || req.AccountID != p.Account || req.AccountEpoch != p.Epoch ||
		req.GroupID != p.Group || req.BindingEpoch != p.BindingEpoch || req.uploadOnly ||
		req.MediaKind != "" || req.MediaUploadID != "" || len(req.Mentions) != 0 || req.ChiliMentionID != "" ||
		req.QuoteEventID != 0 || len(req.ReferenceDigest) != 0 ||
		(f.ProofKind == "private") != (req.PrivateConversation != "") ||
		f.ProofKind == "private" && req.PrivateEventID < 1 || controlledProofDigest([]byte(req.Text)) != f.ProofTextDigest {
		return empty, true, Denied(409, "onboarding_controlled_proof_window_invalid")
	}
	if err = p.checkText(vault, f.ProofKind, req.Text); err != nil {
		return empty, true, err
	}
	return f, true, nil
}

func controlledProofSettingScope(ctx context.Context, q Queryer, vault *security.Vault, actor string,
	req SettingRequest, reserved bool) (controlledExecutionFence, controlledProofState, bool, error) {
	var empty controlledExecutionFence
	var p controlledProofState
	var targets []string
	err := q.QueryRow(ctx, `SELECT target_ids FROM test_windows WHERE id=$1`, req.TestWindowID).Scan(&targets)
	if err == pgx.ErrNoRows {
		return empty, p, false, nil
	}
	if err != nil {
		return empty, p, false, err
	}
	_, _, found, err := controlledProofSelector(targets)
	if !found || err != nil {
		return empty, p, found, err
	}
	f, p, _, err := controlledProofExecution(ctx, q, req.TestWindowID, reserved)
	if err != nil {
		return empty, p, true, err
	}
	if req.business != nil || req.Workspace != "lajiaoliao" || req.Workspace != p.Platform ||
		actor != p.Actor || req.Account != p.Account || req.Epoch != p.Epoch || req.Version != p.AccountVersion ||
		f.ProofKind != "group" || req.Change != (nativeauth.SettingChange{Kind: "send_text", Group: p.NativeGroup, Text: req.Change.Text}) ||
		controlledProofDigest([]byte(req.Change.Text)) != f.ProofTextDigest {
		return empty, p, true, Denied(409, "onboarding_controlled_proof_window_invalid")
	}
	if err = p.checkText(vault, f.ProofKind, req.Change.Text); err != nil {
		return empty, p, true, err
	}
	return f, p, true, nil
}

func (p controlledProofState) requireSettingConnection(ctx context.Context, q Queryer, req SettingRequest,
	proof *SettingEvidence, expectedGroup string, expectedBinding int64) error {
	if expectedGroup != "" && (expectedGroup != p.Group || expectedBinding != p.BindingEpoch) ||
		req.ReceiverOwner == "" || req.ReceiverRevision < 1 {
		return Denied(409, "onboarding_controlled_proof_window_invalid")
	}
	var live bool
	err := q.QueryRow(ctx, `SELECT enabled AND owner=$2 AND revision=$3 AND owner<>''
		AND lease_until>clock_timestamp()+interval '3 seconds'
		AND observed_at>clock_timestamp()-interval '15 seconds' AND observed_at<=clock_timestamp()
		AND snapshot->>'state'='connected' FROM account_receivers WHERE account_id=$1 FOR SHARE`,
		p.Account, req.ReceiverOwner, req.ReceiverRevision).Scan(&live)
	if err == pgx.ErrNoRows || err == nil && !live {
		return Denied(409, "native_text_connection_changed")
	}
	if err != nil {
		return err
	}
	if proof != nil && (!proof.ModerationAllowed || proof.UID != p.Sender ||
		proof.Role != "owner" && proof.Role != "admin" || p.Role != "" && proof.Role != p.Role ||
		proof.Target != "" || (proof.ReceiverOwner != "" || proof.ReceiverRevision != 0) &&
		(proof.ReceiverOwner != req.ReceiverOwner || proof.ReceiverRevision != req.ReceiverRevision)) {
		return Denied(409, "onboarding_controlled_proof_window_invalid")
	}
	return nil
}
