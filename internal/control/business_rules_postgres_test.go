package control

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wangshangliao/internal/database"
	"wangshangliao/internal/features"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

// The database and every native port in this file are synthetic. These tests
// are serial and can reset only wsl_test, never a running business database.
func businessIntegrationDB(t *testing.T) *database.DB {
	t.Helper()
	if os.Getenv("WSL_INTEGRATION") != "1" {
		t.Skip("set WSL_INTEGRATION=1 to run isolated business rule PostgreSQL tests")
	}
	raw, err := os.ReadFile("../../.env")
	if err != nil {
		t.Fatal("integration configuration unavailable")
	}
	env := make(map[string]string)
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok && !strings.HasPrefix(key, "#") {
			env[key] = value
		}
	}
	dsn := func(key, name string) string {
		u, err := url.Parse(env[key])
		if err != nil || u.Hostname() != "127.0.0.1" {
			t.Fatal("isolated tests require explicit localhost URLs")
		}
		u.Path = "/" + name
		if os.Getenv("WSL_CONTAINER_TEST") == "1" {
			u.Host = "postgres:5432"
		}
		return u.String()
	}
	ctx := context.Background()
	owner, err := pgx.Connect(ctx, dsn("MIGRATION_DATABASE_URL", "postgres"))
	if err != nil {
		t.Fatal("cannot connect to isolated database owner")
	}
	var exists bool
	err = owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname='wsl_test')`).Scan(&exists)
	if err == nil && !exists {
		_, err = owner.Exec(ctx, `CREATE DATABASE wsl_test OWNER wsl_owner`)
	}
	owner.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reset, err := pgx.Connect(ctx, dsn("MIGRATION_DATABASE_URL", "wsl_test"))
	if err != nil {
		t.Fatal("cannot connect to isolated database")
	}
	var name string
	if err = reset.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil || name != "wsl_test" {
		t.Fatal("refusing to reset a non-test database")
	}
	_, err = reset.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public; REVOKE CREATE ON SCHEMA public FROM PUBLIC`)
	reset.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, dsn("MIGRATION_DATABASE_URL", "wsl_test")); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(ctx, dsn("DATABASE_URL", "wsl_test"), dsn("TENANT_DATABASE_URL", "wsl_test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

type businessFixture struct {
	t        *testing.T
	ctx      context.Context
	db       *database.DB
	service  *BusinessRulesService
	tenant   string
	actor    string
	plan     string
	config   features.Config
	roles    atomic.Int32
	reads    atomic.Int32
	writes   atomic.Int32
	attempts atomic.Int32
	queries  atomic.Int64
}

type businessQueryCounter struct{ count *atomic.Int64 }

func (c businessQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.count.Add(1)
	return ctx
}

func (businessQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func newBusinessFixture(t *testing.T) *businessFixture {
	t.Helper()
	f := &businessFixture{t: t, db: businessIntegrationDB(t), ctx: context.Background()}
	config := f.db.Platform.Config()
	config.ConnConfig.Tracer = businessQueryCounter{count: &f.queries}
	traced, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	f.db.Platform.Close()
	f.db.Platform = traced
	vault, err := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{73}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	f.config = features.Config{
		BlockedWords: []string{"bad"}, Blacklist: []string{"blocked"}, Exemptions: []string{"exempt"},
		KeywordReplies: []features.KeywordRule{{Keyword: "hello", Reply: "synthetic single reply"}},
		FeatureOptions: map[string]features.Options{
			"blocked_words": {Enabled: true}, "blacklist": {Enabled: true},
			"exemptions": {Enabled: true}, "keyword_replies": {Enabled: true},
		},
	}
	f.service = &BusinessRulesService{DB: f.db, Vault: vault, Catalog: BusinessPermissionCatalog()}
	f.service.Conversation = func(_ context.Context, scope BusinessScope) (string, error) {
		return "synthetic:" + scope.NativeGroup, nil
	}
	f.service.ReadSource = func(_ BusinessScope, _ string, _ int64, projection, _ []byte) (BusinessMessage, error) {
		f.reads.Add(1)
		var message BusinessMessage
		err := json.Unmarshal(projection, &message)
		return message, err
	}
	f.service.FreshRole = func(ctx context.Context, scope BusinessScope, target string) (BusinessEvidence, error) {
		f.roles.Add(1)
		return f.proof(ctx, scope, target)
	}
	f.service.Execute = f.execute
	f.tenant, f.actor = f.customer()
	return f
}

func (f *businessFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.Platform.Exec(f.ctx, query, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *businessFixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.db.Platform.QueryRow(f.ctx, query, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *businessFixture) customer() (tenant, actor string) {
	f.t.Helper()
	tenant, actor = NewID(), NewID()
	if f.plan == "" {
		f.plan = NewID()
		f.exec(`INSERT INTO plans(id,name,group_limit,ai_group_limit,allowed_platforms,modules)
			VALUES($1,'synthetic business',300,0,ARRAY['wsl','lajiaoliao'],ARRAY['moderation','replies'])`, f.plan)
	}
	f.exec(`INSERT INTO tenants(id,name) VALUES($1,'synthetic')`, tenant)
	f.exec(`INSERT INTO users(id,tenant_id,username,password_hash,role,tenant_role)
		VALUES($1::uuid,$2,$1::uuid::text,'synthetic-unused-password','user','owner')`, actor, tenant)
	f.exec(`UPDATE tenants SET owner_id=$2 WHERE id=$1`, tenant, actor)
	var permissions []string
	for _, p := range f.service.Catalog {
		permissions = append(permissions, p.Code)
	}
	f.exec(`INSERT INTO user_access(user_id,role_code,permissions) VALUES($1,'manager',$2)`, actor, permissions)
	f.exec(`INSERT INTO tenant_capability_ceilings(tenant_id,permissions) VALUES($1,$2)`, tenant, permissions)
	f.exec(`INSERT INTO subscriptions(tenant_id,plan_id,expires_at) VALUES($1,$2,clock_timestamp()+interval '30 days')`, tenant, f.plan)
	return tenant, actor
}

func (f *businessFixture) account(platform string, index int) string {
	f.t.Helper()
	id := NewID()
	nativeID := fmt.Sprint(10000 + index)
	f.exec(`INSERT INTO accounts(id,name,platform,purpose,workspace,auth_state,connection_enabled,native_uid,credential_sealed)
		VALUES($1::uuid,$1::uuid::text,$2,'management',$2,'authenticated',false,$3,$4)`, id, platform, nativeID, []byte("synthetic-sealed-credential"))
	f.exec(`INSERT INTO account_receivers(account_id,enabled,owner,revision,lease_until,observed_at,snapshot)
		VALUES($1,true,'synthetic-native-owner',1,clock_timestamp()+interval '10 minutes',clock_timestamp(),'{"state":"connected"}')`, id)
	if platform == "wsl" {
		f.exec(`INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,$1,1)
			ON CONFLICT(singleton) DO NOTHING`, []byte("synthetic-sealed-deployment"))
	}
	for _, action := range []string{features.BusinessRecall, features.BusinessMute, features.BusinessReply} {
		f.exec(`INSERT INTO business_capability_gates(platform,action,source,state,evidence_refs)
			VALUES($1,$2,'automatic','open',ARRAY['synthetic-only']) ON CONFLICT DO NOTHING`, platform, action)
	}
	return id
}

func (f *businessFixture) group(platform, account, tenant string, index int, enabled bool) BusinessScope {
	f.t.Helper()
	group := NewID()
	native := fmt.Sprint(20000 + index)
	raw, _ := json.Marshal(f.config)
	f.exec(`INSERT INTO native_groups(id,platform,native_id,name) VALUES($1,$2,$3,'synthetic test group')`, group, platform, native)
	f.exec(`INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,management_enabled,config)
		VALUES($1,$2,$3,true,$4::jsonb)`, group, tenant, account, raw)
	if !enabled {
		return BusinessScope{Platform: platform, Group: group, Account: account, Tenant: tenant, NativeGroup: native}
	}
	f.exec(`UPDATE group_business_runtime SET enabled=true,version=version+1 WHERE group_id=$1`, group)
	f.exec(`UPDATE account_receivers SET observed_at=clock_timestamp() WHERE account_id=$1`, account)
	scope, _, err := f.service.scope(f.ctx, f.db.Platform, group)
	if err != nil {
		f.t.Fatal(err)
	}
	f.exec(`INSERT INTO business_rule_consumers(group_id,tenant_id,account_id,platform,binding_epoch,runtime_version,starts_at)
		VALUES($1,$2,$3,$4,$5,$6,$7)`, scope.Group, scope.Tenant, scope.Account, scope.Platform,
		scope.BindingEpoch, scope.RuntimeVersion, scope.EnabledAt)
	return scope
}

func (f *businessFixture) proof(ctx context.Context, scope BusinessScope, sender string) (BusinessEvidence, error) {
	proof := BusinessEvidence{Platform: scope.Platform, Account: scope.Account, Group: scope.NativeGroup,
		SelfID: scope.NativeSelf, Target: sender, OwnerID: "native-owner", SelfRole: "admin", TargetRole: "member",
		AccountEpoch: scope.AccountEpoch, AccountVersion: scope.AccountVersion,
		DeploymentVersion: scope.DeploymentVersion, ObservedAt: time.Now()}
	var credential []byte
	err := f.db.Platform.QueryRow(ctx, `SELECT credential_sealed FROM accounts WHERE id=$1`, scope.Account).Scan(&credential)
	copy(proof.CredentialDigest[:], security.Digest(string(credential)))
	return proof, err
}

func (f *businessFixture) execute(ctx context.Context, in BusinessAttempt, fence BusinessFence) (bool, error) {
	f.attempts.Add(1)
	if in.ClientID == "" || in.Owner == "" || in.Revision < 1 {
		return false, errors.New("missing durable attempt identity")
	}
	if in.Scope.Platform == "wsl" && in.Action == features.BusinessReply {
		if in.Nonce == 0 || in.AAD == 0 || in.RecordRoute == nil {
			return false, errors.New("missing shared nonce or route")
		}
		if err := in.RecordRoute(ctx, nativewire.TextRoute{Digest: bytes.Repeat([]byte{1}, 32),
			DeploymentVersion: in.Scope.DeploymentVersion, ObservedAt: time.Now()}); err != nil {
			return false, err
		}
	} else if in.Nonce != 0 || in.AAD != 0 {
		return false, errors.New("another platform borrowed WSL counters")
	}
	proof, err := f.proof(ctx, in.Scope, in.Sender)
	if err != nil {
		return false, err
	}
	err = fence(ctx, proof, func(context.Context) error {
		f.writes.Add(1)
		return nil
	})
	if err != nil {
		return false, err
	}
	if fence(ctx, proof, func(context.Context) error { f.writes.Add(1); return nil }) == nil {
		return false, errors.New("fence allowed a second write")
	}
	return true, nil
}

func (f *businessFixture) source(scope BusinessScope, sender, text string, event int64) BusinessMessage {
	return BusinessMessage{EventID: event, ReceivedAt: time.Now(), Group: scope.NativeGroup, Sender: sender,
		Text: text, ClientID: fmt.Sprint("synthetic-source-", event),
		Reference: &nativeauth.MessageReference{Platform: scope.Platform, Group: scope.NativeGroup,
			Sender: sender, EventID: event, CreatedAt: time.Now().UnixMilli()}}
}

func (f *businessFixture) queue(scope BusinessScope, sender, text string, event int64) string {
	f.t.Helper()
	if err := f.service.consumeMessages(f.ctx, scope, f.config, []BusinessMessage{f.source(scope, sender, text, event)}); err != nil {
		f.t.Fatal(err)
	}
	var id string
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT id::text FROM business_rule_jobs
		WHERE group_id=$1 AND event_id=$2`, scope.Group, event).Scan(&id); err != nil {
		f.t.Fatal("job was not queued", err)
	}
	return id
}

func (f *businessFixture) state(id string) string {
	f.t.Helper()
	var state, code string
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT status,result_code FROM business_rule_jobs WHERE id=$1`, id).Scan(&state, &code); err != nil {
		f.t.Fatal(err)
	}
	return state + ":" + code
}

func TestPostgresBusinessRulesDurableDualPlatform(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, rule := range []string{"recall", "mute", "reply"} {
			t.Run(platform+"/"+rule, func(t *testing.T) {
				f := newBusinessFixture(t)
				account := f.account(platform, 1)
				scope := f.group(platform, account, f.tenant, 1, true)
				sender, text, action := "human", "hello", features.BusinessReply
				if rule == "recall" {
					text, action = "bad hello", features.BusinessRecall
				} else if rule == "mute" {
					sender, text, action = "blocked", "ordinary message", features.BusinessMute
				}
				job := f.queue(scope, sender, text, 1)
				if err := f.service.execute(f.ctx, job); err != nil {
					t.Fatal(err)
				}
				if f.writes.Load() != 1 || f.attempts.Load() != 1 || f.state(job) != "succeeded:business_native_acknowledged" {
					t.Fatal("expected exactly one confirmed effect", f.writes.Load(), f.state(job))
				}
				f.exec(`UPDATE business_rule_account_cooldowns SET next_write_at='-infinity' WHERE account_id=$1`, account)
				if err := f.service.consumeMessages(f.ctx, scope, f.config, []BusinessMessage{f.source(scope, sender, text, 1)}); err != nil {
					t.Fatal(err)
				}
				if err := f.service.execute(f.ctx, job); err != nil {
					t.Fatal(err)
				}
				if f.writes.Load() != 1 || f.count(`SELECT count(*) FROM business_rule_attempts`) != 1 ||
					f.count(`SELECT count(*) FROM business_rule_jobs`) != 1 {
					t.Fatal("duplicate event replayed an effect")
				}
				if action != features.BusinessRecall {
					before := f.roles.Load()
					if err := f.service.consumeMessages(f.ctx, scope, f.config, []BusinessMessage{f.source(scope, sender, text, 2)}); err != nil {
						t.Fatal(err)
					}
					if f.roles.Load() != before || f.count(`SELECT count(*) FROM business_rule_receipts WHERE decision='cooldown'`) != 1 ||
						f.count(`SELECT count(*) FROM business_rule_jobs`) != 1 {
						t.Fatal("cooling event performed a role read or queued work")
					}
				}
				if rule == "mute" {
					if f.count(`SELECT count(*) FROM business_rule_member_cooldowns
						WHERE member_id='blocked' AND until_at>clock_timestamp()+interval '9 minutes'`) != 1 {
						t.Fatal("member mute period was not persisted")
					}
					fresh := f.service.FreshRole
					currentRole := "member"
					f.service.FreshRole = func(ctx context.Context, scope BusinessScope, target string) (BusinessEvidence, error) {
						proof, err := fresh(ctx, scope, target)
						proof.TargetRole = currentRole
						return proof, err
					}
					// The same keyword could be a cooling mute for an ordinary
					// member or a permitted reply after promotion. Read the
					// actual role without submitting another mute.
					for _, tc := range []struct {
						role, decision string
						event          int64
						replies        int
					}{
						{"member", "cooldown", 3, 0},
						{"admin", "queued", 4, 1},
					} {
						currentRole = tc.role
						before := f.roles.Load()
						if err := f.service.consumeMessages(f.ctx, scope, f.config,
							[]BusinessMessage{f.source(scope, sender, "hello", tc.event)}); err != nil {
							t.Fatal(err)
						}
						if f.roles.Load() != before+1 ||
							f.count(`SELECT count(*) FROM business_rule_receipts WHERE event_id=$1 AND decision=$2`, tc.event, tc.decision) != 1 ||
							f.count(`SELECT count(*) FROM business_rule_jobs WHERE action='mute_member'`) != 1 ||
							f.count(`SELECT count(*) FROM business_rule_jobs WHERE action='send_text' AND status='queued'`) != tc.replies ||
							f.count(`SELECT count(*) FROM business_rule_attempts`) != 1 || f.writes.Load() != 1 {
							t.Fatal("keyword role refresh repeated a mute or missed the changed role", tc.role)
						}
					}
				}
				err := f.db.ActorTenantTx(f.ctx, f.tenant, f.actor, func(tx pgx.Tx) error {
					_, err := tx.Exec(f.ctx, `SELECT * FROM business_rule_jobs`)
					return err
				})
				if err == nil {
					t.Fatal("tenant SQL accessed private jobs")
				}
			})
		}
	}
}

func TestPostgresBusinessRulesFinalFenceChanges(t *testing.T) {
	for _, mode := range []string{"credential", "epoch", "permission", "configuration", "runtime", "role", "receiver", "gate"} {
		t.Run(mode, func(t *testing.T) {
			f := newBusinessFixture(t)
			account := f.account("lajiaoliao", 1)
			scope := f.group("lajiaoliao", account, f.tenant, 1, true)
			sender := "human"
			if mode == "role" {
				sender = "blocked"
			}
			job := f.queue(scope, sender, "hello", 1)
			f.service.Execute = func(ctx context.Context, in BusinessAttempt, fence BusinessFence) (bool, error) {
				f.attempts.Add(1)
				proof, err := f.proof(ctx, in.Scope, in.Sender)
				if err != nil {
					return false, err
				}
				query, args := "", []any{}
				switch mode {
				case "credential":
					// Keep versions unchanged to prove the digest is checked too.
					query, args = `UPDATE accounts SET credential_sealed=$2 WHERE id=$1`, []any{account, []byte("different-sealed-credential")}
				case "epoch":
					query, args = `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, []any{account}
				case "permission":
					query, args = `UPDATE user_access SET permissions=array_remove(permissions,'group_action:send_text') WHERE user_id=$1`, []any{f.actor}
				case "configuration":
					query, args = `UPDATE group_bindings SET version=version+1 WHERE group_id=$1`, []any{scope.Group}
				case "runtime":
					query, args = `UPDATE group_business_runtime SET enabled=false,version=version+1 WHERE group_id=$1`, []any{scope.Group}
				case "role":
					proof.TargetRole = "admin"
					query = `SELECT 1`
				case "receiver":
					query, args = `UPDATE account_receivers SET owner='new-connection-owner' WHERE account_id=$1`, []any{account}
				case "gate":
					query, args = `UPDATE business_capability_gates SET state='closed',version=version+1 WHERE platform='lajiaoliao' AND action='send_text' AND source='automatic'`, nil
				}
				if _, err = f.db.Platform.Exec(ctx, query, args...); err != nil {
					return false, err
				}
				err = fence(ctx, proof, func(context.Context) error { f.writes.Add(1); return nil })
				return err == nil, err
			}
			if err := f.service.execute(f.ctx, job); err != nil {
				t.Fatal(err)
			}
			if f.writes.Load() != 0 || !strings.HasPrefix(f.state(job), "failed:") {
				t.Fatal("changed authorization or native identity escaped final fence", f.state(job))
			}
			if f.count(`SELECT count(*) FROM business_rule_attempts WHERE submitted_at IS NOT NULL`) != 0 {
				t.Fatal("rejected final evidence consumed a native submission")
			}
		})
	}
}

func TestPostgresBusinessRulesUnknownNeverResent(t *testing.T) {
	f := newBusinessFixture(t)
	account := f.account("lajiaoliao", 1)
	scope := f.group("lajiaoliao", account, f.tenant, 1, true)
	job := f.queue(scope, "blocked", "hello", 1)
	confirmed := f.service.Execute
	var original BusinessAttempt
	f.service.Execute = func(ctx context.Context, in BusinessAttempt, fence BusinessFence) (bool, error) {
		original = in
		if ok, err := confirmed(ctx, in, fence); !ok || err != nil {
			return ok, err
		}
		return false, context.DeadlineExceeded
	}
	if err := f.service.execute(f.ctx, job); err != nil {
		t.Fatal(err)
	}
	if f.state(job) != "unknown:business_write_unconfirmed" || f.writes.Load() != 1 {
		t.Fatal("unknown write was not retained", f.state(job))
	}
	for i := 0; i < 2; i++ {
		if err := f.service.Recover(f.ctx); err != nil {
			t.Fatal(err)
		}
		if err := f.service.execute(f.ctx, job); err != nil {
			t.Fatal(err)
		}
		ids, err := f.service.dispatchable(f.ctx)
		if err != nil || len(ids) != 0 {
			t.Fatal("unknown account was scheduled", ids, err)
		}
	}
	f.service.Observe = func(_ context.Context, in BusinessAttempt) (bool, error) {
		if in.JobID != original.JobID || in.ClientID != original.ClientID ||
			in.Action != original.Action || in.Target != original.Target {
			return false, errors.New("reconciliation changed original attempt")
		}
		return true, nil
	}
	if err := f.service.Reconcile(f.ctx, Actor{ID: f.actor, TenantID: f.tenant, Role: "user"}, scope.Group, job); err != nil {
		t.Fatal(err)
	}
	if f.writes.Load() != 1 || f.attempts.Load() != 1 || f.state(job) != "succeeded:business_original_effect_observed" {
		t.Fatal("reconciliation repeated an effect", f.state(job))
	}
}
