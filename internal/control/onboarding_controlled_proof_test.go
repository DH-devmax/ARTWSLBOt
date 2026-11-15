package control

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

func proofWindowFixture(t *testing.T, platform, kind string) (*security.Vault, controlledProofState, controlledProofWindow, string) {
	t.Helper()
	vault, err := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{39}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	token, prefix := strings.Repeat("A", 43), "群绑定 "
	if kind == "private" {
		token, prefix = strings.Repeat("B", 43), "群接入 "
	}
	p := controlledProofState{
		Challenge: NewID(), Actor: NewID(), Tenant: NewID(), Platform: platform, Scope: NewID(),
		Group: NewID(), NativeGroup: "12345", Account: NewID(), Sender: "10001",
		Receiver: NewID(), Recipient: "10002", ChallengeVersion: 4, ScopeVersion: 2,
		Epoch: 3, AccountVersion: 7, ReceiverEpoch: 5, ReceiverVersion: 9, BindingEpoch: 6, BindingVersion: 8,
		ExpiresAt: time.Now().Add(10 * time.Minute), ScopeExpiresAt: time.Now().Add(time.Hour),
		TokenHash: bindingTokenDigest(token), Role: "owner",
	}
	if kind == "group" {
		p.Identity, err = vault.Seal("group-binding-identity:"+p.Challenge, []byte(p.Sender))
		if err != nil {
			t.Fatal(err)
		}
	}
	p.Binding = onboardingControlledBinding{Tenant: p.Tenant, Account: p.Receiver, Epoch: p.BindingEpoch, Version: p.BindingVersion}
	text, action := prefix+token, "send_text"
	if kind == "private" {
		action = "send_private_text"
	}
	w := controlledProofWindow{ID: NewID(), Actor: p.Actor, Platform: platform, Group: p.Group,
		Admission: "native_debug", Accounts: []string{p.Account}, Actions: []string{action}, Budget: 1,
		Epochs:    map[string]int64{p.Account: p.Epoch, p.Receiver: p.ReceiverEpoch},
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(2 * time.Minute)}
	w.Targets = p.targets(kind, controlledProofDigest([]byte(text)), controlledProofDigest([]byte("synthetic-request")))
	return vault, p, w, text
}

func proofStateRow(p controlledProofState) capabilityRow {
	return capabilityRow{values: []any{
		p.Challenge, p.Actor, p.Tenant, p.Platform, p.Scope, p.Group, p.NativeGroup,
		p.Account, p.Sender, p.Receiver, p.Recipient, p.ChallengeVersion, p.ScopeVersion,
		p.Epoch, p.AccountVersion, p.ReceiverEpoch, p.ReceiverVersion, p.BindingEpoch, p.BindingVersion,
		p.ExpiresAt, p.ScopeExpiresAt, p.TokenHash, p.Identity, p.OwnerHash, p.Role,
	}}
}

func proofWindowRow(w controlledProofWindow) capabilityRow {
	epochs, _ := json.Marshal(w.Epochs)
	return capabilityRow{values: []any{
		w.ID, w.Actor, w.Platform, w.Group, w.Admission, w.Accounts, w.Targets, w.Actions,
		epochs, w.Budget, w.Used, w.Cancelled, w.CreatedAt, w.ExpiresAt,
	}}
}

func proofExecutionQueries(t *testing.T, p controlledProofState, w controlledProofWindow) *capabilityQueries {
	t.Helper()
	return &capabilityQueries{t: t, rows: []capabilityRow{
		proofWindowRow(w), proofStateRow(p),
		{values: []any{p.Binding.Tenant, p.Binding.Account, p.Binding.Epoch, p.Binding.Version, true}},
		{values: []any{p.Role}}, {values: []any{1, true}},
	}}
}

func TestOnboardingControlledProofInputAndMarkers(t *testing.T) {
	_, p, w, text := proofWindowFixture(t, "wsl", "private")
	in := OnboardingControlledProofInput{Version: p.ChallengeVersion, Kind: "private", Text: text,
		Seconds: 300, Key: "synthetic-proof-key"}
	if !controlledProofInputValid(p.Challenge, in) {
		t.Fatal("valid proof input rejected")
	}
	for _, mutate := range []func(*OnboardingControlledProofInput){
		func(in *OnboardingControlledProofInput) { in.Kind = "send_text" },
		func(in *OnboardingControlledProofInput) { in.Text = "arbitrary text" },
		func(in *OnboardingControlledProofInput) { in.Text = "群绑定 " + strings.Repeat("B", 43) },
		func(in *OnboardingControlledProofInput) { in.Text += "\n" },
		func(in *OnboardingControlledProofInput) { in.Version = 0 },
		func(in *OnboardingControlledProofInput) { in.Seconds = 301 },
		func(in *OnboardingControlledProofInput) { in.Seconds = 4 },
		func(in *OnboardingControlledProofInput) { in.Key = "" },
		func(in *OnboardingControlledProofInput) { in.Key = "contains space" },
	} {
		bad := in
		mutate(&bad)
		_, err := (&GroupBindingService{}).CreateControlledProofWindow(context.Background(), SessionRef{}, p.Challenge, bad)
		capabilityError(t, err, "onboarding_controlled_proof_invalid")
	}
	_, err := (&GroupBindingService{}).CreateControlledProofWindow(context.Background(), SessionRef{}, p.Challenge, in)
	capabilityError(t, err, "group_binding_native_unavailable")
	challenge, kind, found, err := controlledProofSelector(w.Targets)
	if err != nil || !found || challenge != p.Challenge || kind != "private" {
		t.Fatal("proof marker did not identify its challenge and kind", err)
	}
	for _, targets := range [][]string{
		{controlledProofPrefix + "invalid:group"},
		{controlledProofPrefix + p.Challenge + ":media"},
		{controlledProofPrefix + p.Challenge + ":group:extra"},
		{w.Targets[1], w.Targets[1]},
	} {
		_, _, found, err := controlledProofSelector(targets)
		if !found || err == nil {
			t.Fatal("ambiguous or forged marker treated as an ordinary window")
		}
	}
	if _, _, found, err := controlledProofSelector([]string{"10002", "ordinary-target"}); found || err != nil {
		t.Fatal("ordinary native grant acquired proof admission")
	}
}

func TestOnboardingControlledProofDiscoveryInput(t *testing.T) {
	service := &NativeTextService{}
	account := NewID()
	for _, input := range []struct{ account, kind, target string }{
		{"", "private", "receiver"},
		{account, "", "receiver"},
		{account, "send_text", "receiver"},
		{account, "group", ""},
		{account, "group", strings.Repeat("x", 201)},
		{account, "private", "receiver\n"},
	} {
		_, err := service.OnboardingControlledProofWindows(context.Background(), SessionRef{},
			input.account, input.kind, input.target)
		capabilityError(t, err, "onboarding_controlled_proof_invalid")
	}
	_, err := service.OnboardingControlledProofWindows(context.Background(), SessionRef{}, account, "private", "receiver")
	capabilityError(t, err, "group_binding_native_unavailable")
}

func TestOnboardingControlledProofWindowExactSingleton(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, kind := range []string{"private", "group"} {
			t.Run(platform+"/"+kind, func(t *testing.T) {
				_, p, w, _ := proofWindowFixture(t, platform, kind)
				f, err := w.verify(p, kind, false)
				if err != nil || f.ProofWindowID != w.ID || f.ProofAccountID != p.Account ||
					f.ControlledScopeID != p.Scope || f.ProofChallengeVersion != p.ChallengeVersion {
					t.Fatal("exact proof window rejected or lost its persistent fence", err, f)
				}
				w.Used = 1
				if _, err = w.verify(p, kind, true); err != nil {
					t.Fatal("reserved proof attempt rejected", err)
				}
				if _, err = w.verify(p, kind, false); err == nil {
					t.Fatal("consumed proof grant authorized another attempt")
				}
				w.Used = 0
				for name, mutate := range map[string]func(*controlledProofWindow){
					"wrong actor":       func(w *controlledProofWindow) { w.Actor = NewID() },
					"wrong platform":    func(w *controlledProofWindow) { w.Platform = "other" },
					"wrong group":       func(w *controlledProofWindow) { w.Group = NewID() },
					"receiver source":   func(w *controlledProofWindow) { w.Accounts = []string{p.Receiver} },
					"extra source":      func(w *controlledProofWindow) { w.Accounts = append(w.Accounts, p.Receiver) },
					"wrong epoch":       func(w *controlledProofWindow) { w.Epochs[p.Account]++ },
					"receiver epoch":    func(w *controlledProofWindow) { w.Epochs[p.Receiver]++ },
					"extra epoch":       func(w *controlledProofWindow) { w.Epochs[NewID()] = 1 },
					"more budget":       func(w *controlledProofWindow) { w.Budget = 2 },
					"overused":          func(w *controlledProofWindow) { w.Used = 2 },
					"cancelled":         func(w *controlledProofWindow) { w.Cancelled = true },
					"expired":           func(w *controlledProofWindow) { w.ExpiresAt = time.Now() },
					"too long":          func(w *controlledProofWindow) { w.ExpiresAt = w.CreatedAt.Add(301 * time.Second) },
					"challenge expiry":  func(w *controlledProofWindow) { w.CreatedAt = p.ExpiresAt; w.ExpiresAt = p.ExpiresAt.Add(time.Second) },
					"package admission": func(w *controlledProofWindow) { w.Admission = "package" },
					"arbitrary target":  func(w *controlledProofWindow) { w.Targets[0] = "other-native-target" },
					"changed version":   func(w *controlledProofWindow) { w.Targets[2] = "onboarding_proof_version:999" },
					"changed scope":     func(w *controlledProofWindow) { w.Targets[3] += "0" },
					"changed binding":   func(w *controlledProofWindow) { w.Targets[4] += "0" },
					"invalid hash":      func(w *controlledProofWindow) { w.Targets[5] = "onboarding_proof_text:xx" },
					"no request":        func(w *controlledProofWindow) { w.Targets = w.Targets[:8] },
					"extra action":      func(w *controlledProofWindow) { w.Actions = append(w.Actions, "send_image") },
					"wrong action":      func(w *controlledProofWindow) { w.Actions = []string{"send_image"} },
				} {
					t.Run(name, func(t *testing.T) {
						bad := w
						bad.Targets, bad.Accounts, bad.Actions = slices.Clone(w.Targets), slices.Clone(w.Accounts), slices.Clone(w.Actions)
						bad.Epochs = map[string]int64{p.Account: p.Epoch, p.Receiver: p.ReceiverEpoch}
						mutate(&bad)
						if _, err := bad.verify(p, kind, false); err == nil {
							t.Fatal("mutated proof window accepted")
						}
					})
				}
			})
		}
	}
}

func TestOnboardingControlledProofTextHashAndRealIdentity(t *testing.T) {
	vault, p, _, text := proofWindowFixture(t, "lajiaoliao", "group")
	if err := p.checkText(vault, "group", text); err != nil {
		t.Fatal("real sealed applicant identity rejected", err)
	}
	capabilityError(t, p.checkText(vault, "private", text), "onboarding_controlled_proof_text_changed")
	capabilityError(t, p.checkText(vault, "group", text+" "), "onboarding_controlled_proof_text_changed")
	wrong := p
	wrong.Sender = "other-native-user"
	capabilityError(t, wrong.checkText(vault, "group", text), "group_binding_identity_changed")
	wrong = p
	wrong.Challenge = NewID()
	capabilityError(t, wrong.checkText(vault, "group", text), "group_binding_identity_changed")
	capabilityError(t, p.checkText(nil, "group", text), "group_binding_native_unavailable")
}

func TestOnboardingControlledProofPlainTextOnly(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, kind := range []string{"private", "group"} {
			t.Run(platform+"/"+kind, func(t *testing.T) {
				vault, p, w, text := proofWindowFixture(t, platform, kind)
				req := TextRequest{AccountID: p.Account, GroupID: p.Group, AccountEpoch: p.Epoch,
					BindingEpoch: p.BindingEpoch, TestWindowID: w.ID, Text: text}
				if kind == "private" {
					req.PrivateConversation, req.PrivateEventID = strings.Repeat("c", 64), 17
				}
				f, found, err := controlledProofTextScope(context.Background(), proofExecutionQueries(t, p, w),
					vault, p.Actor, platform, req, false, w.Targets)
				if err != nil || !found || f.ProofTarget != p.target(kind) {
					t.Fatal("pure exact proof rejected", err)
				}
				for name, mutate := range map[string]func(*TextRequest){
					"arbitrary text": func(r *TextRequest) { r.Text = "send anything" },
					"source":         func(r *TextRequest) { r.AccountID = p.Receiver },
					"group":          func(r *TextRequest) { r.GroupID = NewID() },
					"epoch":          func(r *TextRequest) { r.AccountEpoch++ },
					"binding":        func(r *TextRequest) { r.BindingEpoch++ },
					"media":          func(r *TextRequest) { r.MediaKind = "image" },
					"upload":         func(r *TextRequest) { r.MediaUploadID = NewID() },
					"upload only":    func(r *TextRequest) { r.uploadOnly = true },
					"mention":        func(r *TextRequest) { r.Mentions = []nativewire.MessageMention{{UID: 10002}} },
					"chili mention":  func(r *TextRequest) { r.ChiliMentionID = "10002" },
					"quote":          func(r *TextRequest) { r.QuoteEventID = 1 },
					"reference":      func(r *TextRequest) { r.ReferenceDigest = []byte("another-message") },
					"scene": func(r *TextRequest) {
						if kind == "private" {
							r.PrivateConversation = ""
						} else {
							r.PrivateConversation, r.PrivateEventID = strings.Repeat("c", 64), 17
						}
					},
				} {
					t.Run(name, func(t *testing.T) {
						bad := req
						mutate(&bad)
						_, found, err := controlledProofTextScope(context.Background(), proofExecutionQueries(t, p, w),
							vault, p.Actor, platform, bad, false, w.Targets)
						if !found || err == nil {
							t.Fatal("proof window became a generic message grant")
						}
					})
				}
				_, _, err = controlledProofTextScope(context.Background(), proofExecutionQueries(t, p, w),
					vault, NewID(), platform, req, false, w.Targets)
				capabilityError(t, err, "onboarding_controlled_proof_window_invalid")
				_, _, err = controlledProofTextScope(context.Background(), proofExecutionQueries(t, p, w),
					vault, p.Actor, "other", req, false, w.Targets)
				capabilityError(t, err, "onboarding_controlled_proof_window_invalid")
			})
		}
	}
}

func TestOnboardingControlledProofSettingOnlyChiliGroup(t *testing.T) {
	vault, p, w, text := proofWindowFixture(t, "lajiaoliao", "group")
	req := SettingRequest{Account: p.Account, Workspace: p.Platform, Epoch: p.Epoch, Version: p.AccountVersion,
		TestWindowID: w.ID, Change: nativeauth.SettingChange{Kind: "send_text", Group: p.NativeGroup, Text: text}}
	query := func() *capabilityQueries {
		q := proofExecutionQueries(t, p, w)
		q.rows = append([]capabilityRow{{values: []any{w.Targets}}}, q.rows...)
		return q
	}
	if _, _, found, err := controlledProofSettingScope(context.Background(), query(), vault, p.Actor, req, false); err != nil || !found {
		t.Fatal("exact Chili settings proof rejected", err)
	}
	for name, mutate := range map[string]func(*SettingRequest){
		"platform": func(r *SettingRequest) { r.Workspace = "wsl" },
		"version":  func(r *SettingRequest) { r.Version++ },
		"source":   func(r *SettingRequest) { r.Account = p.Receiver },
		"group":    func(r *SettingRequest) { r.Change.Group = "another-group" },
		"text":     func(r *SettingRequest) { r.Change.Text = "arbitrary text" },
		"action":   func(r *SettingRequest) { r.Change.Kind = "publish_notice" },
		"target":   func(r *SettingRequest) { r.Change.Target = p.Recipient },
		"enabled":  func(r *SettingRequest) { r.Change.Enabled = true },
		"minutes":  func(r *SettingRequest) { r.Change.Minutes = 1 },
		"business": func(r *SettingRequest) { r.business = &assistantBusiness{} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := req
			mutate(&bad)
			_, _, found, err := controlledProofSettingScope(context.Background(), query(), vault, p.Actor, bad, false)
			if !found || err == nil {
				t.Fatal("settings proof granted another action or business identity")
			}
		})
	}
}

func TestOnboardingControlledProofRechecksLiveStateAndOrigin(t *testing.T) {
	_, p, w, _ := proofWindowFixture(t, "wsl", "group")
	for _, scenario := range []string{"missing", "database", "stale role", "role changed", "duplicate", "not prepared"} {
		t.Run(scenario, func(t *testing.T) {
			q := proofExecutionQueries(t, p, w)
			switch scenario {
			case "missing":
				q.rows = q.rows[:2]
				q.rows[1] = capabilityRow{err: pgx.ErrNoRows}
			case "database":
				q.rows = q.rows[:2]
				q.rows[1] = capabilityRow{err: errors.New("synthetic database unavailable")}
			case "stale role":
				q.rows = q.rows[:4]
				q.rows[3] = capabilityRow{err: pgx.ErrNoRows}
			case "role changed":
				q.rows = q.rows[:4]
				q.rows[3] = capabilityRow{values: []any{"admin"}}
			case "duplicate":
				q.rows[4] = capabilityRow{values: []any{2, true}}
			case "not prepared":
				q.rows[4] = capabilityRow{values: []any{1, false}}
			}
			if _, _, found, err := controlledProofExecution(context.Background(), q, w.ID, false); !found || err == nil {
				t.Fatal("scope, fresh role, singleton or server preparation check omitted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := controlledProofExecution(ctx, proofExecutionQueries(t, p, w), w.ID, false); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled proof window continued reading", err)
	}
	w.Targets = []string{"ordinary-window-target"}
	q := &capabilityQueries{t: t, rows: []capabilityRow{proofWindowRow(w)}}
	if _, _, found, err := controlledProofExecution(context.Background(), q, w.ID, false); found || err != nil || q.calls != 1 {
		t.Fatal("ordinary window acquired onboarding checks", err)
	}
}

func TestOnboardingControlledProofSettingConnectionAndRole(t *testing.T) {
	_, p, _, _ := proofWindowFixture(t, "lajiaoliao", "group")
	req := SettingRequest{ReceiverOwner: "synthetic-owner", ReceiverRevision: 2}
	proof := SettingEvidence{UID: p.Sender, Role: "owner", ModerationAllowed: true}
	yes := func() *capabilityQueries {
		return &capabilityQueries{t: t, rows: []capabilityRow{{values: []any{true}}}}
	}
	if err := p.requireSettingConnection(context.Background(), yes(), req, &proof, p.Group, p.BindingEpoch); err != nil {
		t.Fatal("current snapshot and receiver binding rejected", err)
	}
	for name, mutate := range map[string]func(*SettingEvidence){
		"sender":     func(e *SettingEvidence) { e.UID = p.Recipient },
		"ordinary":   func(e *SettingEvidence) { e.Role = "member" },
		"changed":    func(e *SettingEvidence) { e.Role = "admin" },
		"absent":     func(e *SettingEvidence) { e.ModerationAllowed = false },
		"target":     func(e *SettingEvidence) { e.Target = p.Recipient },
		"owner":      func(e *SettingEvidence) { e.ReceiverOwner = "another-owner" },
		"generation": func(e *SettingEvidence) { e.ReceiverRevision = 3 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := proof
			mutate(&bad)
			if err := p.requireSettingConnection(context.Background(), yes(), req, &bad, p.Group, p.BindingEpoch); err == nil {
				t.Fatal("changed live native evidence accepted")
			}
		})
	}
	no := &capabilityQueries{t: t, rows: []capabilityRow{{values: []any{false}}}}
	capabilityError(t, p.requireSettingConnection(context.Background(), no, req, &proof, "", 0), "native_text_connection_changed")
	capabilityError(t, p.requireSettingConnection(context.Background(), nil, req, &proof, NewID(), p.BindingEpoch),
		"onboarding_controlled_proof_window_invalid")
}

func TestOnboardingControlledProofFinalFenceAndHistoricalCompatibility(t *testing.T) {
	_, p, w, _ := proofWindowFixture(t, "wsl", "group")
	w.Used = 1
	f, err := w.verify(p, "group", true)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.require(context.Background(), proofExecutionQueries(t, p, w), p.Actor, p.Platform, p.Group); err != nil {
		t.Fatal("matching final proof snapshot rejected", err)
	}
	changed := f
	changed.ProofChallengeVersion++
	capabilityError(t, changed.require(context.Background(), proofExecutionQueries(t, p, w), p.Actor, p.Platform, p.Group),
		"onboarding_controlled_proof_window_invalid")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q := proofExecutionQueries(t, p, w)
	q.rows = append(q.rows, capabilityRow{values: []any{false}})
	capabilityError(t, f.requireSubmission(ctx, q, 2*time.Second), "onboarding_controlled_proof_window_invalid")
	q = proofExecutionQueries(t, p, w)
	q.rows = append(q.rows, capabilityRow{values: []any{true}})
	if err = f.requireSubmission(ctx, q, 2*time.Second); err != nil {
		t.Fatal("covered proof write deadline rejected", err)
	}
	if _, _, err = f.modelContext(context.Background(), nil); err == nil {
		t.Fatal("proof send fixture became a model grant")
	}
	var historical TextRequest
	if err = json.Unmarshal([]byte(`{"AccountEpoch":1,"BindingEpoch":2}`), &historical); err != nil ||
		historical.controlledExecutionFence != (controlledExecutionFence{}) {
		t.Fatal("historical text acquired proof privileges", err)
	}
	raw, err := json.Marshal(TextRequest{})
	if err != nil || bytes.Contains(raw, []byte("onboarding_proof")) {
		t.Fatal("ordinary persisted format changed", err)
	}
}
