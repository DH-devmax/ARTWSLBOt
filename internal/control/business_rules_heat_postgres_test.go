package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wangshangliao/internal/database"
	"wangshangliao/internal/features"
)

func (f *businessFixture) heatMessage(scope BusinessScope, sender, text string) (HeatScope, HeatMessage) {
	return HeatScope{Platform: scope.Platform, Tenant: scope.Tenant, Group: scope.Group, NativeGroup: scope.NativeGroup},
		HeatMessage{Group: scope.NativeGroup, Sender: sender, Text: text, ReceivedAt: time.Now()}
}

func (f *businessFixture) saveRuleConfig(group string, config features.Config) {
	f.t.Helper()
	raw, err := json.Marshal(config)
	if err != nil {
		f.t.Fatal(err)
	}
	f.exec(`UPDATE group_bindings SET config=$2::jsonb,version=version+1 WHERE group_id=$1`, group, raw)
}

func TestPostgresBusinessRulesHeatFullAndCoolingRequireRole(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newBusinessFixture(t)
			account := f.account(platform, 1)
			for index, tc := range []struct {
				name, state, sender, role, text string
				adminOnly                       bool
				want                            bool
			}{
				{"full_unknown", "full", "human", "", "bad", false, false},
				{"full_admin", "full", "human", "admin", "bad", false, false},
				{"full_owner", "full", "human", "owner", "bad", false, false},
				{"full_exempt", "full", "exempt", "member", "bad", false, false},
				{"full_verified_member", "full", "human", "member", "bad", false, true},
				{"cooling_unknown", "cooling", "human", "", "bad", false, false},
				{"cooling_admin", "cooling", "human", "admin", "bad", false, false},
				{"cooling_exempt", "cooling", "exempt", "member", "bad", false, false},
				{"cooling_verified_member", "cooling", "human", "member", "bad", false, true},
				{"cooling_admin_only_member", "cooling", "human", "member", "hello", true, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					f.exec(`DELETE FROM business_rule_jobs; DELETE FROM business_rule_receipts;
						DELETE FROM business_rule_account_cooldowns`)
					scope := f.group(platform, account, f.tenant, index+1, true)
					if tc.state == "full" {
						f.queue(scope, "human", "hello", 1)
					} else {
						f.exec(`INSERT INTO business_rule_account_cooldowns(account_id,next_write_at)
							VALUES($1,clock_timestamp()+interval '1 minute')`, account)
					}
					if tc.adminOnly {
						config := f.config
						config.FeatureOptions = map[string]features.Options{
							"keyword_replies": {Enabled: true, AdminOnly: true},
						}
						f.saveRuleConfig(scope.Group, config)
					}
					read := f.service.FreshRole
					f.service.FreshRole = func(ctx context.Context, scope BusinessScope, target string) (BusinessEvidence, error) {
						proof, err := read(ctx, scope, target)
						proof.TargetRole = tc.role
						if tc.role == "owner" {
							proof.OwnerID = target
						}
						return proof, err
					}
					defer func() { f.service.FreshRole = read }()
					before, jobs := f.roles.Load(), f.count(`SELECT count(*) FROM business_rule_jobs`)
					chat, message := f.heatMessage(scope, tc.sender, tc.text)
					got, err := f.service.SuppressesHeat(f.ctx, chat, message)
					if err != nil || got != tc.want || f.roles.Load()-before != 1 {
						t.Fatal("full/cooling replaced fresh role or exemption checks", got, tc.want, err, f.roles.Load()-before)
					}
					if f.count(`SELECT count(*) FROM business_rule_jobs`) != jobs ||
						f.writes.Load() != 0 || f.attempts.Load() != 0 {
						t.Fatal("heat suppression created or executed automatic work")
					}
				})
			}
		})
	}
}

func TestPostgresBusinessRulesHeatAdmissionAndUnavailableScope(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newBusinessFixture(t)
			account := f.account(platform, 1)
			for index, mode := range []string{"gate_closed", "ceiling_denied", "permission_denied", "receiver_disabled",
				"runtime_disabled", "ownership_paused", "role_read_failed", "unknown_self", "nonmatch", "wrong_chat_scope"} {
				t.Run(mode, func(t *testing.T) {
					scope := f.group(platform, account, f.tenant, index+1, true)
					read := f.service.FreshRole
					defer func() { f.service.FreshRole = read }()
					chat, message := f.heatMessage(scope, "human", "hello")
					roleReads := int32(1)
					switch mode {
					case "gate_closed":
						f.exec(`UPDATE business_capability_gates SET state='closed',version=version+1
							WHERE platform=$1 AND action='send_text' AND source='automatic'`, platform)
						defer f.exec(`UPDATE business_capability_gates SET state='open',version=version+1
							WHERE platform=$1 AND action='send_text' AND source='automatic'`, platform)
					case "ceiling_denied":
						f.exec(`UPDATE tenant_capability_ceilings SET permissions=array_remove(permissions,'group_action:send_text'),
							version=version+1 WHERE tenant_id=$1`, f.tenant)
						defer f.exec(`UPDATE tenant_capability_ceilings SET permissions=array_append(permissions,'group_action:send_text'),
							version=version+1 WHERE tenant_id=$1`, f.tenant)
					case "permission_denied":
						f.exec(`UPDATE user_access SET permissions=array_remove(permissions,'group_action:send_text') WHERE user_id=$1`, f.actor)
						defer f.exec(`UPDATE user_access SET permissions=array_append(permissions,'group_action:send_text') WHERE user_id=$1`, f.actor)
					case "receiver_disabled":
						f.exec(`UPDATE account_receivers SET enabled=false WHERE account_id=$1`, account)
						defer f.exec(`UPDATE account_receivers SET enabled=true WHERE account_id=$1`, account)
						roleReads = 0
					case "runtime_disabled":
						f.exec(`UPDATE group_business_runtime SET enabled=false,version=version+1 WHERE group_id=$1`, scope.Group)
						roleReads = 0
					case "ownership_paused":
						f.ownership(scope)
						f.exec(`UPDATE group_binding_ownership SET state='paused',version=version+1 WHERE group_id=$1`, scope.Group)
					case "role_read_failed":
						f.service.FreshRole = func(context.Context, BusinessScope, string) (BusinessEvidence, error) {
							f.roles.Add(1)
							return BusinessEvidence{}, errors.New("synthetic native unavailable")
						}
					case "unknown_self":
						f.service.FreshRole = func(ctx context.Context, scope BusinessScope, target string) (BusinessEvidence, error) {
							proof, err := read(ctx, scope, target)
							proof.SelfRole = ""
							return proof, err
						}
					case "nonmatch":
						message.Text, roleReads = "no literal match", 0
					case "wrong_chat_scope":
						chat.Tenant, roleReads = NewID(), 0
					}
					before := f.roles.Load()
					got, err := f.service.SuppressesHeat(f.ctx, chat, message)
					if err != nil || got || f.roles.Load()-before != roleReads {
						t.Fatal("unavailable rules suppressed or interrupted heat", mode, got, err, f.roles.Load()-before)
					}
					// The caller can continue to its next ordinary message; a
					// rejected management candidate is not a chat-batch error.
					message.Text = "next ordinary message"
					got, err = f.service.SuppressesHeat(f.ctx, chat, message)
					if err != nil || got || f.roles.Load()-before != roleReads {
						t.Fatal("failed candidate blocked the following heat message", mode, got, err)
					}
					if f.count(`SELECT count(*) FROM business_rule_jobs`) != 0 ||
						f.attempts.Load() != 0 || f.writes.Load() != 0 {
						t.Fatal("suppression bypassed the independent automatic worker")
					}
				})
			}
		})
	}
}

type businessHeatRaceKey struct{}

type businessHeatConfigRace struct {
	changed atomic.Bool
	change  func()
}

func (r *businessHeatConfigRace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, businessHeatRaceKey{},
		strings.HasPrefix(data.SQL, "SELECT b.config,b.version,b.epoch,r.version FROM group_business_runtime"))
}

func (r *businessHeatConfigRace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if target, _ := ctx.Value(businessHeatRaceKey{}).(bool); target && r.changed.CompareAndSwap(false, true) {
		r.change()
	}
}

func TestPostgresBusinessRulesHeatConfigurationRaces(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, phase := range []string{"before_scope", "during_role"} {
			t.Run(platform+"/"+phase, func(t *testing.T) {
				f := newBusinessFixture(t)
				account := f.account(platform, 1)
				scope := f.group(platform, account, f.tenant, 1, true)
				config := f.config
				config.KeywordReplies = []features.KeywordRule{{Keyword: "different", Reply: "new rule"}}
				change := func() { f.saveRuleConfig(scope.Group, config) }
				if phase == "before_scope" {
					race := &businessHeatConfigRace{change: change}
					poolConfig := f.db.Platform.Config()
					poolConfig.ConnConfig.Tracer = race
					pool, err := pgxpool.NewWithConfig(f.ctx, poolConfig)
					if err != nil {
						t.Fatal(err)
					}
					defer pool.Close()
					// The tracer's update uses the fixture's other pool; it
					// changes configuration after the first row was read.
					f.service.DB = &database.DB{Platform: pool, Tenant: f.db.Tenant}
				} else {
					read := f.service.FreshRole
					f.service.FreshRole = func(ctx context.Context, scope BusinessScope, target string) (BusinessEvidence, error) {
						proof, err := read(ctx, scope, target)
						change()
						return proof, err
					}
				}
				chat, message := f.heatMessage(scope, "human", "hello")
				got, err := f.service.SuppressesHeat(f.ctx, chat, message)
				wantReads := int32(0)
				if phase == "during_role" {
					wantReads = 1
				}
				if err != nil || got || f.roles.Load() != wantReads {
					t.Fatal("mixed configuration versions suppressed or interrupted heat", got, err, f.roles.Load())
				}
				if f.count(`SELECT count(*) FROM business_rule_jobs`) != 0 || f.writes.Load() != 0 {
					t.Fatal("configuration race submitted an automatic action")
				}
			})
		}
	}
}
