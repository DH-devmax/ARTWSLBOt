package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgxpool"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

type syntheticOnboardingOutboundClient struct {
	target                     nativeauth.OnboardingOutboundTarget
	result                     nativeauth.OnboardingOutboundEvidence
	lookups, sends, reconciles int
	submissions                atomic.Int32
	last                       nativeauth.OnboardingOutboundRequest
	err                        error
	skipFence, duplicateFence  bool
}

func (c *syntheticOnboardingOutboundClient) Lookup(context.Context, string) (nativeauth.OnboardingOutboundTarget, error) {
	c.lookups++
	return c.target, c.err
}

func (c *syntheticOnboardingOutboundClient) Send(ctx context.Context, req nativeauth.OnboardingOutboundRequest,
	fence nativeauth.OnboardingOutboundFence) (nativeauth.OnboardingOutboundEvidence, error) {
	c.sends++
	c.last = req
	if c.skipFence {
		return c.result, nil
	}
	submit := func(context.Context) error {
		c.submissions.Add(1)
		return c.err
	}
	err := fence(ctx, c.target, submit)
	if err == nil && c.duplicateFence {
		err = fence(ctx, c.target, submit)
	}
	return c.result, err
}

func (c *syntheticOnboardingOutboundClient) Reconcile(_ context.Context,
	req nativeauth.OnboardingOutboundRequest) (nativeauth.OnboardingOutboundEvidence, error) {
	c.reconciles++
	c.last = req
	return c.result, c.err
}

func (*syntheticOnboardingOutboundClient) Close() {}

func onboardingOutboundFixture(platform string) (OnboardingOutboundRequest, SettingSnapshot, *syntheticOnboardingOutboundClient) {
	self, target, account := "7", "8", "456"
	if platform == "lajiaoliao" {
		self, target, account = "self-string", "target-string", "public-string"
	}
	req := OnboardingOutboundRequest{
		OnboardingNativeScope: OnboardingNativeScope{Platform: platform, AccountID: "synthetic-account",
			AccountEpoch: 3, AccountVersion: 4},
		OperationID: "11111111-2222-4333-8444-555555555555", SelfID: self,
		Target:    OnboardingOutboundTarget{NativeID: target, AddAccount: account, Name: "synthetic target"},
		ExpiresAt: time.Now().Add(time.Minute),
	}
	base := SettingSnapshot{Platform: platform, SelfID: self, AccountEpoch: req.AccountEpoch,
		AccountVersion: req.AccountVersion, DeploymentVersion: 5, SampledAt: time.Now().UTC()}
	client := &syntheticOnboardingOutboundClient{
		target: nativeauth.OnboardingOutboundTarget{ID: target, Account: account, Name: req.Target.Name, CanRequest: true},
		result: nativeauth.OnboardingOutboundEvidence{OperationID: req.OperationID,
			SenderID: self, RecipientID: target, State: "pending", ApplicationID: "11"},
	}
	return req, base, client
}

func outboundFixtureCheck(ctx context.Context, scope OnboardingNativeScope, base SettingSnapshot) error {
	if !scope.matches(base) {
		return ErrMemberScope
	}
	return ctx.Err()
}

func TestOnboardingOutboundLookupIsScopedAndAlreadyFriendsIsNotIdentityProof(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, friends := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/friends=%t", platform, friends), func(t *testing.T) {
				req, base, client := onboardingOutboundFixture(platform)
				client.target.IsFriend = friends
				client.target.CanRequest = !friends
				result, err := readOnboardingOutbound(context.Background(), OnboardingOutboundLookup{
					OnboardingNativeScope: req.OnboardingNativeScope, AddAccount: req.Target.AddAccount,
				}, nil, client, base, outboundFixtureCheck)
				if err != nil || result.Target.AlreadyFriend != friends || result.Target.NativeID != req.Target.NativeID ||
					result.SelfID != req.SelfID || result.SampledAt.IsZero() || result.ApplicationID != "" ||
					client.lookups != 1 || client.sends != 0 || client.reconciles != 0 {
					t.Fatal("lookup changed scope, mutated, or fabricated application evidence", err)
				}
				if friends && result.State != "friends" || !friends && result.State != "available" {
					t.Fatal("lookup relationship state was not explicit")
				}
			})
		}
	}
}

func TestOnboardingOutboundIncompleteRelationshipKeepsSafeReason(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		req, base, client := onboardingOutboundFixture(platform)
		client.target.CanRequest = false
		client.target.BlockedReason = "native_onboarding_outbound_read_incomplete"
		result, err := readOnboardingOutbound(context.Background(), OnboardingOutboundLookup{
			OnboardingNativeScope: req.OnboardingNativeScope, AddAccount: req.Target.AddAccount,
		}, nil, client, base, outboundFixtureCheck)
		var native *nativeauth.Error
		if !errors.As(err, &native) || native.Code != "native_onboarding_outbound_read_incomplete" ||
			result != (OnboardingOutboundSnapshot{}) || client.lookups != 1 || client.sends != 0 ||
			client.reconciles != 0 || client.submissions.Load() != 0 {
			t.Fatal("incomplete relationship lost its safe reason or submitted an application", err)
		}
	}
}

func TestOnboardingOutboundReadsRejectLateOrSubstitutedEvidence(t *testing.T) {
	for _, mode := range []string{"platform", "epoch", "version", "account", "target", "self", "name",
		"pending", "revoked", "cancelled", "upstream"} {
		t.Run(mode, func(t *testing.T) {
			req, base, client := onboardingOutboundFixture("wsl")
			switch mode {
			case "platform":
				base.Platform = "lajiaoliao"
			case "epoch":
				base.AccountEpoch++
			case "version":
				base.AccountVersion++
			case "account":
				client.target.Account = "999"
			case "target":
				client.target.ID = ""
			case "self":
				client.target.ID = base.SelfID
			case "name":
				client.target.Name = "invalid\nname"
			case "pending":
				client.target.CanRequest = false
				client.target.BlockedReason = "native_onboarding_outbound_pending"
			case "upstream":
				client.err = errors.New("synthetic upstream failure")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			check := func(ctx context.Context, scope OnboardingNativeScope, snapshot SettingSnapshot) error {
				if mode == "revoked" {
					return ErrMemberScope
				}
				return outboundFixtureCheck(ctx, scope, snapshot)
			}
			result, err := readOnboardingOutbound(ctx, OnboardingOutboundLookup{
				OnboardingNativeScope: req.OnboardingNativeScope, AddAccount: req.Target.AddAccount,
			}, nil, client, base, check)
			if err == nil || result != (OnboardingOutboundSnapshot{}) || client.sends != 0 {
				t.Fatal("unverified lookup or late data escaped")
			}
			if (mode == "platform" || mode == "epoch" || mode == "version" || mode == "cancelled") &&
				client.lookups != 0 {
				t.Fatal("invalid scope reached the native lookup")
			}
		})
	}
}

func TestOnboardingOutboundReconcileUsesOriginalOperationWithoutSendingOrRenewal(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, state := range []string{"pending", "friends", "rejected", "missing", "unknown"} {
			t.Run(platform+"/"+state, func(t *testing.T) {
				req, base, client := onboardingOutboundFixture(platform)
				req.ExpiresAt = time.Now().Add(-time.Minute)
				client.result.State, client.result.IsFriend = state, state == "friends"
				result, err := readOnboardingOutbound(context.Background(), OnboardingOutboundLookup{
					OnboardingNativeScope: req.OnboardingNativeScope, AddAccount: req.Target.AddAccount,
				}, &req, client, base, outboundFixtureCheck)
				if err != nil || result.State != state || client.reconciles != 1 ||
					client.lookups != 0 || client.sends != 0 || client.submissions.Load() != 0 ||
					client.last.OperationID != req.OperationID || client.last.SelfID != req.SelfID ||
					client.last.Target.ID != req.Target.NativeID || !client.last.ExpiresAt.Equal(req.ExpiresAt) {
					t.Fatal("reconciliation changed original attempt, deadline, or performed a send", err)
				}
			})
		}
	}
	for _, mode := range []string{"operation", "sender", "recipient", "state", "friend_flag"} {
		t.Run(mode, func(t *testing.T) {
			req, base, client := onboardingOutboundFixture("wsl")
			switch mode {
			case "operation":
				client.result.OperationID = "22222222-2222-4222-8222-222222222222"
			case "sender":
				client.result.SenderID = "9"
			case "recipient":
				client.result.RecipientID = "9"
			case "state":
				client.result.State = "approved"
			case "friend_flag":
				client.result.IsFriend = true
			}
			result, err := readOnboardingOutbound(context.Background(), OnboardingOutboundLookup{
				OnboardingNativeScope: req.OnboardingNativeScope, AddAccount: req.Target.AddAccount,
			}, &req, client, base, outboundFixtureCheck)
			if err == nil || result != (OnboardingOutboundSnapshot{}) || client.sends != 0 {
				t.Fatal("unrelated historical application escaped reconciliation")
			}
		})
	}
}

func TestOnboardingOutboundFinalFenceRevocationCancellationExpiryAndUnknown(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, mode := range []string{"sent", "changed_target", "changed_account", "changed_name", "friends",
			"revoked_before", "revoked_final", "expired", "expired_final", "cancelled_final",
			"unknown", "duplicate_submit", "duplicate_fence", "no_fence"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				req, base, client := onboardingOutboundFixture(platform)
				switch mode {
				case "changed_target":
					client.target.ID += "9"
				case "changed_account":
					client.target.Account += "9"
				case "changed_name":
					client.target.Name += " changed"
				case "friends":
					client.target.IsFriend = true
					client.target.CanRequest = false
				case "expired":
					req.ExpiresAt = time.Now().Add(-time.Second)
				case "expired_final":
					req.ExpiresAt = time.Now().Add(20 * time.Millisecond)
				case "unknown":
					client.err = &nativeauth.Error{Code: "native_transport_unknown", Submitted: true}
				case "duplicate_fence":
					client.duplicateFence = true
				case "no_fence":
					client.skipFence = true
				}
				revoked := false
				check := func(ctx context.Context, scope OnboardingNativeScope, snapshot SettingSnapshot) error {
					if mode == "revoked_before" || revoked {
						return ErrMemberScope
					}
					return outboundFixtureCheck(ctx, scope, snapshot)
				}
				fences := 0
				err := sendOnboardingOutbound(context.Background(), req, client, base, check,
					func(work context.Context, evidence OnboardingOutboundSnapshot, submit func(context.Context) error) error {
						fences++
						if evidence.Target != req.Target || evidence.State != "available" ||
							evidence.SelfID != req.SelfID {
							t.Fatal("control final fence received substituted target")
						}
						if mode == "revoked_final" {
							revoked = true
						}
						if mode == "expired_final" {
							timer := time.NewTimer(time.Until(req.ExpiresAt) + time.Millisecond)
							<-timer.C
						}
						if mode == "cancelled_final" {
							cancelled, cancel := context.WithCancel(work)
							cancel()
							return submit(cancelled)
						}
						err := submit(work)
						if mode == "duplicate_submit" {
							return submit(work)
						}
						return err
					})
				wantWrites := int32(0)
				if mode == "sent" || mode == "unknown" || mode == "duplicate_submit" || mode == "duplicate_fence" {
					wantWrites = 1
				}
				if client.submissions.Load() != wantWrites || (mode == "sent") != (err == nil) ||
					client.sends > 1 || fences > 1 {
					t.Fatal("false success, repeated request or submission fence bypass", err)
				}
				if client.sends == 1 && (client.last.OperationID != req.OperationID ||
					client.last.SelfID != req.SelfID || !client.last.ExpiresAt.Equal(req.ExpiresAt)) {
					t.Fatal("native send lost stable operation or deadline")
				}
				if mode == "unknown" {
					var original *nativeauth.Error
					if !errors.As(err, &original) || !original.Submitted || original.Code != "native_transport_unknown" {
						t.Fatal("unknown native outcome was converted to a retryable preparation failure")
					}
				}
			})
		}
	}
}

func TestOnboardingOutboundConcurrentFinalSubmitRunsOnce(t *testing.T) {
	req, base, client := onboardingOutboundFixture("wsl")
	err := sendOnboardingOutbound(context.Background(), req, client, base, outboundFixtureCheck,
		func(work context.Context, _ OnboardingOutboundSnapshot, submit func(context.Context) error) error {
			var successes atomic.Int32
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					if submit(work) == nil {
						successes.Add(1)
					}
				})
			}
			wg.Wait()
			if successes.Load() != 1 {
				t.Fatal("one durable fence allowed concurrent native submissions")
			}
			return nil
		})
	if err != nil || client.submissions.Load() != 1 {
		t.Fatal("single-use final submission failed", err)
	}
}

func TestOnboardingOutboundInvalidReceiptRetainsUnknownAndNeverResends(t *testing.T) {
	for _, mode := range []string{"operation", "sender", "recipient", "state", "friend_flag"} {
		t.Run(mode, func(t *testing.T) {
			req, base, client := onboardingOutboundFixture("wsl")
			switch mode {
			case "operation":
				client.result.OperationID = "22222222-2222-4222-8222-222222222222"
			case "sender":
				client.result.SenderID = "9"
			case "recipient":
				client.result.RecipientID = "9"
			case "state":
				client.result.State = "available"
			case "friend_flag":
				client.result.IsFriend = true
			}
			err := sendOnboardingOutbound(context.Background(), req, client, base, outboundFixtureCheck,
				func(work context.Context, _ OnboardingOutboundSnapshot, submit func(context.Context) error) error {
					return submit(work)
				})
			var failure *nativeauth.Error
			if !errors.As(err, &failure) || failure.Code != "native_response_invalid" || !failure.Submitted ||
				client.sends != 1 || client.submissions.Load() != 1 {
				t.Fatal("invalid submitted receipt became a success or was retried", err)
			}
		})
	}
}

func TestOnboardingOutboundFreshFriendshipDoesNotFabricateSubmission(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		req, base, client := onboardingOutboundFixture(platform)
		client.skipFence = true
		client.result.State, client.result.IsFriend = "friends", true
		fences := 0
		err := sendOnboardingOutbound(context.Background(), req, client, base, outboundFixtureCheck,
			func(context.Context, OnboardingOutboundSnapshot, func(context.Context) error) error {
				fences++
				return nil
			})
		var failure *nativeauth.Error
		if !errors.As(err, &failure) || failure.Code != "native_onboarding_outbound_already_friend" ||
			failure.Submitted || fences != 0 || client.submissions.Load() != 0 || client.sends != 1 {
			t.Fatal("fresh relationship fabricated a submission or requested retry", platform, err)
		}
	}
}

type syntheticWSLOutboundClient struct {
	result nativeauth.OnboardingOutboundEvidence
}

func (syntheticWSLOutboundClient) LookupOnboardingOutboundFriend(context.Context, string) (nativeauth.OnboardingOutboundTarget, error) {
	return nativeauth.OnboardingOutboundTarget{}, nil
}
func (syntheticWSLOutboundClient) SendOnboardingOutboundFriend(context.Context, nativeauth.OnboardingOutboundRequest,
	nativeauth.OnboardingOutboundFence) (nativeauth.OnboardingOutboundEvidence, error) {
	return nativeauth.OnboardingOutboundEvidence{}, nil
}
func (c syntheticWSLOutboundClient) ReconcileOnboardingOutboundFriend(context.Context,
	nativeauth.OnboardingOutboundRequest) (nativeauth.OnboardingOutboundEvidence, error) {
	return c.result, nil
}
func (syntheticWSLOutboundClient) Close() {}

func TestOnboardingOutboundWSLHistoricalApprovalDoesNotBecomeCurrentFriendship(t *testing.T) {
	for _, tc := range []struct{ native, expected string }{
		{"approved", "unknown"}, {"expired", "rejected"}, {"deleted", "rejected"},
		{"pending", "pending"}, {"friends", "friends"}, {"unknown", "unknown"},
	} {
		req, base, _ := onboardingOutboundFixture("wsl")
		raw := syntheticWSLOutboundClient{result: nativeauth.OnboardingOutboundEvidence{
			OperationID: req.OperationID, SenderID: req.SelfID, RecipientID: req.Target.NativeID,
			State: tc.native, IsFriend: tc.native == "friends", ApplicationID: "11",
		}}
		client := wslOnboardingOutboundClient{nativeOnboardingOutboundClient: raw}
		result, err := readOnboardingOutbound(context.Background(), OnboardingOutboundLookup{
			OnboardingNativeScope: req.OnboardingNativeScope, AddAccount: req.Target.AddAccount,
		}, &req, client, base, outboundFixtureCheck)
		if err != nil || result.State != tc.expected || result.Target.AlreadyFriend != (tc.native == "friends") {
			t.Fatal("historical application was used as current friendship", tc.native, err)
		}
	}
}

func TestOnboardingOutboundRequestValidationAndRedaction(t *testing.T) {
	req, base, _ := onboardingOutboundFixture("wsl")
	if !req.valid() {
		t.Fatal("valid scoped request rejected")
	}
	for _, operation := range []string{"", "11111111222243338444555555555555", "g1111111-2222-4333-8444-555555555555",
		"1111111--2222-4333-8444-555555555555", req.OperationID + "\n"} {
		bad := req
		bad.OperationID = operation
		if bad.valid() {
			t.Fatal("invalid durable operation admitted")
		}
	}
	for _, value := range []any{req, OnboardingOutboundLookup{
		OnboardingNativeScope: req.OnboardingNativeScope, AddAccount: req.Target.AddAccount,
	}} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var log bytes.Buffer
		slog.New(slog.NewJSONHandler(&log, nil)).Info("synthetic", "request", value)
		for _, text := range []string{string(raw), fmt.Sprintf("%v %+v %#v", value, value, value), log.String()} {
			if strings.Contains(text, req.Target.AddAccount) || strings.Contains(text, req.Target.Name) ||
				strings.Contains(text, req.OperationID) {
				t.Fatal("target account, name or operation escaped request logging")
			}
		}
	}
	m := &ReceiverManager{}
	_, err := m.LookupOnboardingOutboundFriend(context.Background(), OnboardingOutboundLookup{
		OnboardingNativeScope: req.OnboardingNativeScope, AddAccount: "not-a-wsl-account",
	}, func(work context.Context, read func(context.Context) error) error {
		return read(work)
	})
	var accountError *nativeauth.Error
	if !errors.As(err, &accountError) || accountError.Code != "native_onboarding_outbound_account_invalid" {
		t.Fatal("invalid search account was reported as an ownership change", err)
	}
	for _, bad := range []OnboardingOutboundRequest{{}, {
		OnboardingNativeScope: req.OnboardingNativeScope, SelfID: base.SelfID, Target: req.Target,
	}} {
		if err := m.SendOnboardingOutboundFriend(context.Background(), bad, nil); err == nil {
			t.Fatal("invalid request reached scheduler or database")
		}
	}
}

func TestOnboardingOutboundLookupFenceValidationPrecedesQueue(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, mode := range []string{"nil_fence", "scope", "account"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				req, _, _ := onboardingOutboundFixture(platform)
				lookup := OnboardingOutboundLookup{
					OnboardingNativeScope: req.OnboardingNativeScope, AddAccount: req.Target.AddAccount,
				}
				calls := 0
				var fence OnboardingOutboundLookupFence = func(context.Context, func(context.Context) error) error {
					calls++
					return errors.New("unexpected lookup fence")
				}
				switch mode {
				case "nil_fence":
					fence = nil
				case "scope":
					lookup.AccountEpoch = 0
				case "account":
					lookup.AddAccount = "invalid\naccount"
				}
				// No scheduler or database is installed: validation must precede both.
				result, err := (&ReceiverManager{}).LookupOnboardingOutboundFriend(context.Background(), lookup, fence)
				if result != (OnboardingOutboundSnapshot{}) || calls != 0 {
					t.Fatal("invalid lookup reached its fence or exposed a result", result, calls)
				}
				if mode == "account" {
					var native *nativeauth.Error
					if !errors.As(err, &native) || native.Code != "native_onboarding_outbound_account_invalid" {
						t.Fatal("invalid account lost its specific error", err)
					}
				} else if !errors.Is(err, ErrMemberScope) {
					t.Fatal("invalid scope or nil fence reached the read queue", err)
				}
			})
		}
	}
}

func TestOnboardingOutboundLookupCancelledBeforeQueue(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			req, _, _ := onboardingOutboundFixture(platform)
			reads := NewReadScheduler(context.Background())
			defer reads.Close()
			m := &ReceiverManager{reads: reads}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var fences atomic.Int32
			result, err := m.LookupOnboardingOutboundFriend(ctx, OnboardingOutboundLookup{
				OnboardingNativeScope: req.OnboardingNativeScope, AddAccount: req.Target.AddAccount,
			}, func(context.Context, func(context.Context) error) error {
				fences.Add(1)
				return nil
			})
			if !errors.Is(err, context.Canceled) || result != (OnboardingOutboundSnapshot{}) ||
				fences.Load() != 0 || reads.Snapshot(req.AccountID).Requests60s != 0 {
				t.Fatal("cancelled lookup reached its fence or native transport", err, fences.Load())
			}
		})
	}
}

type outboundLookupQueueFixture struct {
	manager         *ReceiverManager
	reads           *ReadScheduler
	queries, checks atomic.Int32
}

func newOutboundLookupQueueFixture(t *testing.T) *outboundLookupQueueFixture {
	t.Helper()
	f := &outboundLookupQueueFixture{}
	vault, err := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{71}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := vault.Seal("lajiaoliao-session:synthetic-account",
		[]byte(`{"user_id":"self-string","im_token":"synthetic-unused-token"}`))
	if err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig("postgres://synthetic:synthetic@127.0.0.1/synthetic?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	var servers sync.WaitGroup
	config.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		servers.Add(1)
		go func() {
			defer servers.Done()
			defer server.Close()
			backend := pgproto3.NewBackend(server, server)
			if _, err := backend.ReceiveStartupMessage(); err != nil {
				return
			}
			backend.Send(&pgproto3.AuthenticationOk{})
			backend.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
			backend.Send(&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: "on"})
			backend.Send(&pgproto3.BackendKeyData{ProcessID: 1, SecretKey: []byte{0, 0, 0, 1}})
			backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
			if err := backend.Flush(); err != nil {
				return
			}
			for {
				message, err := backend.Receive()
				if err != nil {
					return
				}
				if _, ok := message.(*pgproto3.Terminate); ok {
					return
				}
				query, ok := message.(*pgproto3.Query)
				if !ok {
					t.Error("unexpected synthetic database protocol message")
					return
				}
				f.queries.Add(1)
				sql := strings.Join(strings.Fields(query.String), " ")
				var types []uint32
				var values [][]byte
				credential := []byte(`\x` + hex.EncodeToString(sealed))
				switch {
				case strings.Contains(sql, "LEFT JOIN account_read_state"):
					types, values = []uint32{20, 1184}, [][]byte{[]byte("3"), nil}
				case strings.HasPrefix(sql, "SELECT a.native_uid,a.credential_sealed,a.epoch,a.version,a.connection_enabled,"):
					types = []uint32{25, 17, 20, 20, 16, 25, 20}
					values = [][]byte{[]byte("self-string"), credential, []byte("3"), []byte("4"), []byte("t"),
						[]byte("synthetic-owner"), []byte("11")}
				case strings.HasPrefix(sql, "SELECT credential_sealed FROM accounts"):
					types, values = []uint32{17}, [][]byte{credential}
				case strings.HasPrefix(sql, "SELECT EXISTS(SELECT 1 FROM accounts"):
					current := []byte("t")
					// Opening is allowed; any unexpected callback fails its fresh
					// account check before it can reach a real native client.
					if f.checks.Add(1) > 1 {
						current = []byte("f")
					}
					types, values = []uint32{16}, [][]byte{current}
				default:
					t.Error("unexpected query in synthetic lookup fixture")
					backend.Send(&pgproto3.ErrorResponse{Severity: "ERROR", Code: "XX000",
						Message: "unexpected synthetic query"})
				}
				if types != nil {
					fields := make([]pgproto3.FieldDescription, len(types))
					for i, oid := range types {
						fields[i] = pgproto3.FieldDescription{Name: []byte("value"), DataTypeOID: oid,
							DataTypeSize: -1, TypeModifier: -1, Format: pgproto3.TextFormat}
					}
					backend.Send(&pgproto3.RowDescription{Fields: fields})
					backend.Send(&pgproto3.DataRow{Values: values})
					backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
				}
				backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
				if err := backend.Flush(); err != nil {
					return
				}
			}
		}()
		return client, nil
	}
	// Only net.Pipe is used; this fixture cannot connect to a PostgreSQL server.
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		servers.Wait()
	})
	f.reads = NewReadScheduler(context.Background())
	t.Cleanup(f.reads.Close)
	f.manager = &ReceiverManager{DB: pool, Vault: vault, reads: f.reads}
	return f
}

func TestOnboardingOutboundLookupFenceRunsAfterAccountQueue(t *testing.T) {
	for _, mode := range []string{"revoked", "missing_callback", "cancelled_final", "duplicate_callback", "cancelled_queued"} {
		t.Run(mode, func(t *testing.T) {
			req, _, _ := onboardingOutboundFixture("lajiaoliao")
			f := newOutboundLookupQueueFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			requestCtx, cancelRequest := context.WithCancel(ctx)
			defer cancelRequest()
			entered, release := make(chan struct{}), make(chan struct{})
			blocked := make(chan error, 1)
			go func() {
				blocked <- f.reads.Do(ctx, req.AccountID, "synthetic-blocker", ReadRefresh, func(work context.Context) error {
					close(entered)
					select {
					case <-release:
						return nil
					case <-work.Done():
						return work.Err()
					}
				})
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("account blocker did not start")
			}
			var fences atomic.Int32
			var revoked atomic.Bool
			denied := errors.New("synthetic-controlled-window-revoked")
			want := denied
			if mode == "missing_callback" || mode == "duplicate_callback" {
				want = ErrMemberScope
			} else if mode == "cancelled_final" || mode == "cancelled_queued" {
				want = context.Canceled
			}
			type outcome struct {
				result OnboardingOutboundSnapshot
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := f.manager.LookupOnboardingOutboundFriend(requestCtx, OnboardingOutboundLookup{
					OnboardingNativeScope: req.OnboardingNativeScope, AddAccount: req.Target.AddAccount,
				}, func(work context.Context, read func(context.Context) error) error {
					fences.Add(1)
					if !revoked.Load() || !f.reads.Snapshot(req.AccountID).Running || read == nil {
						return errors.New("fence ran before acquiring the account queue")
					}
					switch mode {
					case "missing_callback":
						return nil
					case "cancelled_final", "duplicate_callback":
						final, stop := context.WithCancel(work)
						stop()
						first := read(final)
						if mode == "duplicate_callback" {
							if !errors.Is(first, context.Canceled) {
								return fmt.Errorf("first callback ignored cancellation: %w", first)
							}
							return read(work)
						}
						return first
					default:
						return denied
					}
				})
				done <- outcome{result: result, err: err}
			}()
			waitFor(t, func() bool {
				return f.reads.Snapshot(req.AccountID).QueueDepth == 1 || fences.Load() > 0
			})
			if fences.Load() != 0 || f.queries.Load() != 0 {
				t.Fatal("queued lookup checked its grant or opened a client before acquiring the account lane")
			}
			revoked.Store(true)
			if mode == "cancelled_queued" {
				cancelRequest()
				waitFor(t, func() bool { return f.reads.Snapshot(req.AccountID).QueueDepth == 0 })
			}
			close(release)
			select {
			case got := <-done:
				wantFences := int32(1)
				if mode == "cancelled_queued" {
					wantFences = 0
				}
				if !errors.Is(got.err, want) || got.result != (OnboardingOutboundSnapshot{}) ||
					fences.Load() != wantFences || f.reads.Snapshot(req.AccountID).Requests60s != 0 {
					t.Fatal("lookup bypassed its queued grant, cancellation or single-use fence",
						got.err, fences.Load(), wantFences)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("queued lookup did not return")
			}
			if err := <-blocked; err != nil && mode != "cancelled_queued" {
				t.Fatal("account blocker failed", err)
			}
			wantChecks := int32(1)
			if mode == "cancelled_queued" {
				wantChecks = 0
			}
			if f.checks.Load() != wantChecks {
				t.Fatal("rejected fence reached the native lookup's fresh account check", f.checks.Load())
			}
			if mode != "cancelled_queued" {
				view := f.reads.Snapshot(req.AccountID)
				if view.CooldownUntil == nil || !view.CooldownUntil.After(time.Now()) {
					t.Fatal("rejected queued lookup lost the account read cooldown")
				}
			}
		})
	}
}
