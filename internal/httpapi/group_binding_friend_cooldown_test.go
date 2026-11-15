package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"wangshangliao/internal/control"
)

func outboundRequireBlocked(t *testing.T, out control.GroupBindingChallenge, err error, code string) {
	t.Helper()
	if err != nil {
		var failure *control.Failure
		if errors.As(err, &failure) && failure.Code == code {
			return
		}
	} else if out.FriendOutbound != nil && out.FriendOutbound.Status == "failed" && out.FriendOutbound.Code == code {
		return
	}
	t.Fatal("outgoing account fence did not reject the operation", out, err, code)
}

func TestGroupBindingOutboundPostgresConsecutiveChallengesShareCooldown(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, platform)
			outboundOpenGate(t, f, platform)
			first := outboundControlledChallenge(t, f, client, platform, "outbound-gap-first")
			first = outboundPrepare(t, f, n, ref, first, "outbound-gap-first-prepare")
			n.target.NativeID += "-second"
			n.target.AddAccount += "-second"
			bob := f.bob
			bob.workspace = platform
			second := outboundControlledChallenge(t, f, bob, platform, "outbound-gap-second")
			second = outboundPrepare(t, f, n, onboardingRef(t, bob, platform), second, "outbound-gap-second-prepare")
			if first.Robot.ID != second.Robot.ID {
				t.Fatal("consecutive challenges must exercise the same native account")
			}
			secondTarget := n.target
			n.target = first.FriendOutbound.Target
			var acknowledged time.Time
			n.onOutboundEffect = func(ctx context.Context, r control.GroupBindingOutboundRequest) error {
				var reserved time.Time
				if err := f.db.Platform.QueryRow(ctx, `SELECT next_write_at,clock_timestamp()
					FROM business_rule_account_cooldowns WHERE account_id=$1`, r.AccountID).
					Scan(&reserved, &acknowledged); err != nil {
					return err
				}
				if reserved.Before(acknowledged.Add(5 * time.Second)) {
					t.Fatal("native effect preceded its committed shared account reservation")
				}
				return nil
			}
			out := outboundSend(t, f.app.GroupBinding, ref, first, "outbound-gap-first-confirm")
			if out.FriendOutbound.Status != "submitted" || acknowledged.IsZero() {
				t.Fatal("first synthetic outgoing request failed", out)
			}
			var next time.Time
			if err := f.db.Platform.QueryRow(context.Background(), `SELECT next_write_at
				FROM business_rule_account_cooldowns WHERE account_id=$1`, first.Robot.ID).Scan(&next); err != nil ||
				next.Before(acknowledged.Add(5*time.Second)) {
				t.Fatal("outgoing acknowledgement lost the shared five-second gap", next, err)
			}
			n.target = secondTarget
			blocked, err := (outboundControlledService{f.app.GroupBinding}).SendOutboundFriend(context.Background(), onboardingRef(t, bob, platform), second.ID,
				control.GroupBindingConfirmation{Version: second.Version, Key: "outbound-gap-second-confirm"})
			outboundRequireBlocked(t, blocked, err, "business_account_cooling")
			if n.outboundEffects.Load() != 1 {
				t.Fatal("the same account dispatched a second native request during cooldown")
			}
			before := outboundCalls(n)
			replay := outboundSend(t, f.app.GroupBinding, ref, first, "outbound-gap-first-confirm")
			if replay.FriendOutbound.ID != out.FriendOutbound.ID || outboundCalls(n) != before {
				t.Fatal("cooldown replaced a completed idempotent result")
			}
		})
	}
}

func TestGroupBindingOutboundPostgresCooldownIsAccountScoped(t *testing.T) {
	f, n, client, ref := outboundFixture(t, "wsl")
	outboundOpenGate(t, f, "wsl")
	first := outboundControlledChallenge(t, f, client, "wsl", "outbound-independent-first")
	first = outboundPrepare(t, f, n, ref, first, "outbound-independent-first-prepare")
	if out := outboundSend(t, f.app.GroupBinding, ref, first, "outbound-independent-first-confirm"); out.FriendOutbound.Status != "submitted" {
		t.Fatal("first account request failed", out)
	}
	secondAccount := control.NewID()
	_, err := f.db.Platform.Exec(context.Background(), `INSERT INTO accounts(id,name,platform,workspace,purpose,
		native_uid,auth_state,credential_sealed,connection_enabled)
		VALUES($1,'Synthetic second outbound robot','wsl','wsl','management',$2,'authenticated',$3,false)`,
		secondAccount, "synthetic-second-bot-"+secondAccount, []byte("synthetic-second-credential"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Platform.Exec(context.Background(), `INSERT INTO account_receivers(account_id,enabled,owner,
		lease_until,observed_at,snapshot) VALUES($1,true,'synthetic-receiver',clock_timestamp()+interval '1 hour',
		clock_timestamp(),'{"state":"connected"}')`, secondAccount)
	if err != nil {
		t.Fatal(err)
	}
	f.accounts["wsl"] = secondAccount
	outboundOpenGate(t, f, "wsl")
	if _, err = f.db.Platform.Exec(context.Background(), `UPDATE account_receivers SET enabled=false WHERE account_id=$1`, first.Robot.ID); err != nil {
		t.Fatal(err)
	}
	bob := f.bob
	bob.workspace = "wsl"
	second := outboundControlledChallenge(t, f, bob, "wsl", "outbound-independent-second")
	if _, err = f.db.Platform.Exec(context.Background(), `UPDATE account_receivers SET enabled=true WHERE account_id=$1`, first.Robot.ID); err != nil {
		t.Fatal(err)
	}
	if second.Robot.ID != secondAccount {
		t.Fatal("independent cooldown fixture selected the wrong account")
	}
	second = outboundPrepare(t, f, n, onboardingRef(t, bob, "wsl"), second, "outbound-independent-second-prepare")
	out := outboundSend(t, f.app.GroupBinding, onboardingRef(t, bob, "wsl"), second, "outbound-independent-second-confirm")
	if out.FriendOutbound.Status != "submitted" || n.outboundEffects.Load() != 2 {
		t.Fatal("one account's cooldown blocked another native account", out)
	}
}

func TestGroupBindingOutboundPostgresCooldownSurvivesServiceReplacement(t *testing.T) {
	f, n, client, ref := outboundFixture(t, "wsl")
	outboundOpenGate(t, f, "wsl")
	first := outboundControlledChallenge(t, f, client, "wsl", "outbound-restart-first")
	first = outboundPrepare(t, f, n, ref, first, "outbound-restart-first-prepare")
	outboundSend(t, f.app.GroupBinding, ref, first, "outbound-restart-first-confirm")
	bob := f.bob
	bob.workspace = "wsl"
	n.target.NativeID += "-restart"
	n.target.AddAccount += "-restart"
	second := outboundControlledChallenge(t, f, bob, "wsl", "outbound-restart-second")
	second = outboundPrepare(t, f, n, onboardingRef(t, bob, "wsl"), second, "outbound-restart-second-prepare")
	// Leave only the committed shared reservation recent, excluding an
	// in-memory throttle and the recent-attempt timestamp fallback.
	if _, err := f.db.Platform.Exec(context.Background(), `UPDATE group_binding_outbound_attempts
		SET updated_at=clock_timestamp()-interval '1 minute' WHERE challenge_id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	restarted := &control.GroupBindingService{DB: f.db, Vault: f.vault, Catalog: f.app.GroupBinding.Catalog, Native: n, Decode: f.decode}
	before := n.outboundEffects.Load()
	out, err := (outboundControlledService{restarted}).SendOutboundFriend(context.Background(), onboardingRef(t, bob, "wsl"), second.ID,
		control.GroupBindingConfirmation{Version: second.Version, Key: "outbound-restart-second-confirm"})
	outboundRequireBlocked(t, out, err, "business_account_cooling")
	if n.outboundEffects.Load() != before {
		t.Fatal("service replacement discarded the persisted account gap")
	}
}

func TestGroupBindingOutboundPostgresSharedWriteCooldownFinalFence(t *testing.T) {
	for _, source := range []string{"automatic", "native-succeeded", "native-failed", "native-pending", "native-old"} {
		t.Run(source, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, "wsl")
			outboundOpenGate(t, f, "wsl")
			c := outboundControlledChallenge(t, f, client, "wsl", "outbound-shared-"+source)
			c = outboundPrepare(t, f, n, ref, c, "outbound-shared-prepare")
			n.beforeOutboundSend = func(ctx context.Context, r control.GroupBindingOutboundRequest) error {
				if source == "automatic" {
					_, err := f.db.Platform.Exec(ctx, `INSERT INTO business_rule_account_cooldowns(account_id,next_write_at)
						VALUES($1,clock_timestamp()+interval '1 minute') ON CONFLICT(account_id)
						DO UPDATE SET next_write_at=EXCLUDED.next_write_at`, r.AccountID)
					return err
				}
				status, at := "succeeded", time.Now()
				switch source {
				case "native-failed":
					status = "failed"
				case "native-pending":
					status = "preparing"
				case "native-old":
					at = at.Add(-time.Minute)
				}
				id := control.NewID()
				_, err := f.db.Platform.Exec(ctx, `INSERT INTO native_setting_operations(id,actor_id,account_id,kind,
					idempotency_key,request_hash,account_epoch,status,expires_at,updated_at)
					VALUES($1,$2,$3,'nickname',$4,$5,$6,$7,clock_timestamp()+interval '1 minute',$8)`,
					id, f.adminID, r.AccountID, "synthetic-outbound-shared-"+id, bytes.Repeat([]byte{31}, 32),
					r.AccountEpoch, status, at)
				return err
			}
			out, err := (outboundControlledService{f.app.GroupBinding}).SendOutboundFriend(context.Background(), ref, c.ID,
				control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-shared-confirm"})
			if source == "native-old" {
				if err != nil || out.FriendOutbound == nil || out.FriendOutbound.Status != "submitted" || n.outboundEffects.Load() != 1 {
					t.Fatal("an old completed native setting blocked a new request", out, err)
				}
				return
			}
			if n.outboundEffects.Load() != 0 {
				t.Fatal("outgoing request escaped the shared native/automatic write fence")
			}
			code := "business_account_cooling"
			if source == "native-pending" {
				code = "group_binding_friend_operation_unresolved"
			}
			outboundRequireBlocked(t, out, err, code)
		})
	}
}

// Synthetic successful attempts are aged past the five-second write gap only;
// their immutable created_at stays current for the hourly and target limits.
func outboundAgeWriteGap(t *testing.T, f *onboardingFixture, account string) {
	t.Helper()
	for _, statement := range []string{
		`UPDATE business_rule_account_cooldowns SET next_write_at=clock_timestamp()-interval '1 second' WHERE account_id=$1`,
		`UPDATE group_binding_outbound_attempts SET updated_at=clock_timestamp()-interval '1 minute' WHERE account_id=$1`,
	} {
		if _, err := f.db.Platform.Exec(context.Background(), statement, account); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGroupBindingOutboundPostgresActorAndTargetRateLimits(t *testing.T) {
	for _, scope := range []string{"actor", "target"} {
		t.Run(scope, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, "wsl")
			outboundOpenGate(t, f, "wsl")
			limit := 3
			if scope == "target" {
				limit = 1
			}
			for i := 0; i <= limit; i++ {
				if scope == "actor" {
					n.target.NativeID = fmt.Sprintf("synthetic-actor-target-%d", i)
					n.target.AddAccount = fmt.Sprintf("synthetic-actor-account-%d", i)
				}
				c := outboundControlledChallenge(t, f, client, "wsl", fmt.Sprintf("outbound-rate-%s-%d", scope, i))
				c = outboundPrepare(t, f, n, ref, c, fmt.Sprintf("outbound-rate-prepare-%s-%d", scope, i))
				before := n.outboundEffects.Load()
				out, err := (outboundControlledService{f.app.GroupBinding}).SendOutboundFriend(context.Background(), ref, c.ID,
					control.GroupBindingConfirmation{Version: c.Version, Key: fmt.Sprintf("outbound-rate-confirm-%s-%d", scope, i)})
				if i == limit {
					friendFailure(t, err, 429, "group_binding_outbound_rate_limited")
					if n.outboundEffects.Load() != before {
						t.Fatal("actor hourly/target ten-minute limit dispatched an excess request", scope, out, err)
					}
					return
				}
				if err != nil || out.FriendOutbound == nil || out.FriendOutbound.Status != "submitted" ||
					n.outboundEffects.Load() != before+1 {
					t.Fatal("synthetic request below the rate limit failed", scope, out, err)
				}
				if _, err = f.app.GroupBinding.Cancel(context.Background(), ref, c.ID, c.Version); err != nil {
					t.Fatal(err)
				}
				outboundAgeWriteGap(t, f, c.Robot.ID)
			}
		})
	}
}

func TestGroupBindingOutboundPostgresAccountHourlyLimitAcrossActors(t *testing.T) {
	f, n, _, _ := outboundFixture(t, "wsl")
	outboundOpenGate(t, f, "wsl")
	actors := []testClient{f.admin}
	for i := 1; i < 4; i++ {
		var id string
		err := f.db.Platform.QueryRow(context.Background(), `INSERT INTO users(username,password_hash,role)
			SELECT $1,password_hash,'superadmin' FROM users WHERE id=$2 RETURNING id::text`,
			fmt.Sprintf("synthetic-outbound-rate-admin-%d", i), f.adminID).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		actors = append(actors, f.client(t, f.admin.base, id))
	}
	for i := 0; i <= 12; i++ {
		client := actors[min(i/3, 3)]
		client.workspace = "wsl"
		if i == 12 {
			client = f.bob
			client.workspace = "wsl"
		}
		ref := onboardingRef(t, client, "wsl")
		n.target.NativeID = fmt.Sprintf("synthetic-account-rate-target-%d", i)
		n.target.AddAccount = fmt.Sprintf("synthetic-account-rate-search-%d", i)
		c := outboundControlledChallenge(t, f, client, "wsl", fmt.Sprintf("outbound-account-rate-%d", i))
		c = outboundPrepare(t, f, n, ref, c, fmt.Sprintf("outbound-account-rate-prepare-%d", i))
		out, err := (outboundControlledService{f.app.GroupBinding}).SendOutboundFriend(context.Background(), ref, c.ID,
			control.GroupBindingConfirmation{Version: c.Version, Key: fmt.Sprintf("outbound-account-rate-confirm-%d", i)})
		if i == 12 {
			friendFailure(t, err, 429, "group_binding_outbound_rate_limited")
			if n.outboundEffects.Load() != 12 {
				t.Fatal("account hourly limit was bypassed by a fresh website actor", out, err)
			}
			return
		}
		if err != nil || out.FriendOutbound == nil || out.FriendOutbound.Status != "submitted" {
			t.Fatal("request below the account limit failed", i, out, err)
		}
		if _, err = f.app.GroupBinding.Cancel(context.Background(), ref, c.ID, c.Version); err != nil {
			t.Fatal(err)
		}
		outboundAgeWriteGap(t, f, c.Robot.ID)
	}
}
