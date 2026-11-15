package control

import (
	"context"
	"slices"
	"strings"
	"testing"

	"wangshangliao/internal/features"
)

func TestPostgresBusinessRulesConfiguredCooldownFences(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, boundary := range []string{"candidate", "queue", "submission"} {
			t.Run(platform+"/"+boundary, func(t *testing.T) {
				f := newBusinessFixture(t)
				f.config.FeatureOptions["keyword_replies"] = features.Options{Enabled: true, CooldownSeconds: 120}
				account := f.account(platform, 1)
				scope := f.group(platform, account, f.tenant, 1, true)
				first := f.queue(scope, "human", "hello", 1)
				if err := f.service.execute(f.ctx, first); err != nil || f.writes.Load() != 1 {
					t.Fatal("initial synthetic reply failed", err)
				}
				f.exec(`UPDATE business_rule_account_cooldowns SET next_write_at='-infinity' WHERE account_id=$1`, account)
				setLastReply := func(seconds int) {
					f.exec(`UPDATE business_rule_consumers SET last_reply_at=clock_timestamp()-make_interval(secs=>$2)
						WHERE group_id=$1`, scope.Group, seconds)
				}
				setLastReply(121)
				switch boundary {
				case "candidate":
					setLastReply(45)
				case "queue":
					f.service.FreshRole = func(ctx context.Context, current BusinessScope, sender string) (BusinessEvidence, error) {
						// A competing reply lands after the cheap candidate check.
						setLastReply(45)
						return f.proof(ctx, current, sender)
					}
				}
				reads := f.roles.Load()
				if err := f.service.consumeMessages(f.ctx, scope, f.config,
					[]BusinessMessage{f.source(scope, "human", "hello", 2)}); err != nil {
					t.Fatal(err)
				}
				if boundary != "submission" {
					if f.count(`SELECT count(*) FROM business_rule_receipts WHERE event_id=2 AND decision='cooldown'`) != 1 ||
						f.count(`SELECT count(*) FROM business_rule_jobs`) != 1 || f.writes.Load() != 1 {
						t.Fatal("configured cooldown was bypassed before queueing")
					}
					if boundary == "candidate" && f.roles.Load() != reads {
						t.Fatal("cooling candidate still fetched native roles")
					}
					return
				}
				var second string
				if err := f.db.Platform.QueryRow(f.ctx,
					`SELECT id::text FROM business_rule_jobs WHERE group_id=$1 AND event_id=2`, scope.Group).Scan(&second); err != nil {
					t.Fatal(err)
				}
				// A queued task cannot outrun a newly established group cooldown.
				setLastReply(45)
				if err := f.service.execute(f.ctx, second); err != nil {
					t.Fatal(err)
				}
				if f.state(second) != "failed:business_rule_cooling" || f.writes.Load() != 1 ||
					f.count(`SELECT count(*) FROM business_rule_attempts WHERE submitted_at IS NOT NULL`) != 1 {
					t.Fatal("final fence used the old fixed thirty-second cooldown", f.state(second))
				}
			})
		}
	}
}

func TestPostgresBusinessRulesExtendedMatchingUsesExistingLedger(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, action := range []string{"combined", "exact"} {
			t.Run(platform+"/"+action, func(t *testing.T) {
				f := newBusinessFixture(t)
				f.config.BlockedWords = nil
				f.config.BlockedRules = []features.BlockedRule{
					{Name: "disabled", Words: []string{"hello", "world"}, Disabled: true},
					{Name: "pair", Words: []string{"hello", "world"}},
				}
				f.config.KeywordReplies = []features.KeywordRule{
					{Keyword: "hello", Reply: "disabled reply", Disabled: true},
					{Keyword: "hello", Reply: "exact reply", Match: "exact"},
				}
				scope := f.group(platform, f.account(platform, 1), f.tenant, 1, true)
				text, expected := "hello world", features.BusinessRecall
				if action == "exact" {
					text, expected = "hello", features.BusinessReply
				}
				job := f.queue(scope, "human", text, 1)
				if err := f.service.execute(f.ctx, job); err != nil {
					t.Fatal(err)
				}
				if f.writes.Load() != 1 || f.state(job) != "succeeded:business_native_acknowledged" ||
					f.count(`SELECT count(*) FROM business_rule_attempts WHERE action=$1 AND submitted_at IS NOT NULL`, expected) != 1 {
					t.Fatal("extended rule did not share durable execution", f.state(job))
				}
			})
		}
	}
}

func TestPostgresBusinessRulesUpgradeLegacyPermissions(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, grants := range []string{"explicit_fields", "legacy_only"} {
			for _, test := range []struct {
				name     string
				text     string
				cooldown int
				action   string
			}{
				{name: "blocked_words", text: "bad hello", action: features.BusinessRecall},
				{name: "keyword_implicit_default", text: "hello", action: features.BusinessReply},
				{name: "keyword_explicit_default", text: "hello", cooldown: 30, action: features.BusinessReply},
			} {
				t.Run(platform+"/"+grants+"/"+test.name, func(t *testing.T) {
					f := newBusinessFixture(t)
					f.config.FeatureOptions["keyword_replies"] = features.Options{Enabled: true, CooldownSeconds: test.cooldown}
					if test.cooldown == 30 {
						f.config.BlockedRules = []features.BlockedRule{}
					}
					f.exec(`UPDATE user_access SET permissions=array_remove(array_remove(permissions,$2::text),$3::text),
						version=version+1 WHERE user_id=$1`, f.actor,
						"feature:blocked_words:rules", "feature:keyword_replies:cooldown_seconds")
					if grants == "legacy_only" {
						f.exec(`UPDATE user_access SET permissions=ARRAY(
							SELECT permission FROM unnest(user_access.permissions) AS granted(permission)
							WHERE permission NOT LIKE 'feature:%'), version=version+1 WHERE user_id=$1`, f.actor)
					}
					f.exec(`UPDATE tenant_capability_ceilings SET permissions=array_remove(array_remove(permissions,$2::text),$3::text),
						version=version+1 WHERE tenant_id=$1`, f.tenant,
						"feature:blocked_words:rules", "feature:keyword_replies:cooldown_seconds")
					scope := f.group(platform, f.account(platform, 1), f.tenant, 1, true)
					for _, alias := range []string{"groups:moderation", "groups:replies"} {
						if !slices.Contains(scope.Authorization, alias) {
							t.Fatal("old ceiling removed a legacy alias", alias)
						}
					}
					if grants == "legacy_only" {
						for _, permission := range scope.Authorization {
							if strings.HasPrefix(permission, "feature:") {
								t.Fatal("legacy-only fixture retained a field grant", permission)
							}
						}
						if !slices.Contains(scope.Authorization, "group_action:"+test.action) {
							t.Fatal("legacy-only fixture lost its independent action grant", test.action)
						}
					}
					for _, field := range []struct{ feature, key string }{
						{feature: "blocked_words", key: "rules"},
						{feature: "keyword_replies", key: "cooldown_seconds"},
					} {
						definition, ok := features.Find(field.feature)
						if !ok || features.CanEdit(scope.Authorization, definition, field.key) {
							t.Fatal("upgrade fixture retained an extension field grant", field.feature, field.key)
						}
					}
					job := f.queue(scope, "human", test.text, 1)
					if err := f.service.execute(f.ctx, job); err != nil {
						t.Fatal(err)
					}
					if f.writes.Load() != 1 || f.attempts.Load() != 1 ||
						f.state(job) != "succeeded:business_native_acknowledged" ||
						f.count(`SELECT count(*) FROM business_rule_attempts WHERE action=$1 AND submitted_at IS NOT NULL`, test.action) != 1 {
						t.Fatal("legacy rule required unused extension permissions", f.state(job), f.attempts.Load(), f.writes.Load())
					}
				})
			}
		}
	}
}

func TestPostgresBusinessRulesUpgradeExtendedPermissionsRequired(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, test := range []struct {
			feature string
			field   string
			legacy  string
			text    string
		}{
			{feature: "blocked_words", field: "rules", legacy: "groups:moderation", text: "hello world"},
			{feature: "keyword_replies", field: "cooldown_seconds", legacy: "groups:replies", text: "hello"},
		} {
			for _, missing := range []string{"user_access", "tenant_ceiling"} {
				t.Run(platform+"/"+test.feature+"/"+missing, func(t *testing.T) {
					f := newBusinessFixture(t)
					if test.feature == "blocked_words" {
						f.config.BlockedWords = nil
						f.config.BlockedRules = []features.BlockedRule{{Name: "pair", Words: []string{"hello", "world"}}}
					} else {
						f.config.FeatureOptions["keyword_replies"] = features.Options{Enabled: true, CooldownSeconds: 120}
					}
					permission := features.Permission(test.feature, test.field)
					code := "tenant_capability_denied"
					if missing == "user_access" {
						f.exec(`UPDATE user_access SET permissions=array_remove(permissions,$2::text),
							version=version+1 WHERE user_id=$1`, f.actor, permission)
						code = "permission_denied"
					} else {
						f.exec(`UPDATE tenant_capability_ceilings SET permissions=array_remove(permissions,$2::text),
							version=version+1 WHERE tenant_id=$1`, f.tenant, permission)
					}
					scope := f.group(platform, f.account(platform, 1), f.tenant, 1, true)
					if !slices.Contains(scope.Authorization, test.legacy) {
						t.Fatal("missing extension grant closed the old legacy alias", test.legacy)
					}
					definition, ok := features.Find(test.feature)
					if !ok || features.CanEdit(scope.Authorization, definition, test.field) {
						t.Fatal("missing-permission fixture retained an effective extension grant", permission)
					}
					if err := f.service.consumeMessages(f.ctx, scope, f.config,
						[]BusinessMessage{f.source(scope, "human", test.text, 1)}); err != nil {
						t.Fatal(err)
					}
					blocked := f.count(`SELECT count(*) FROM business_rule_receipts
						WHERE group_id=$1 AND event_id=1 AND rule=$2 AND decision='blocked' AND result_code=$3`,
						scope.Group, test.feature, code)
					jobs := f.count(`SELECT count(*) FROM business_rule_jobs`)
					attempts := f.count(`SELECT count(*) FROM business_rule_attempts`)
					if blocked != 1 || jobs != 0 || attempts != 0 || f.attempts.Load() != 0 || f.writes.Load() != 0 {
						t.Fatalf("extension without %s: blocked=%d jobs=%d attempts=%d calls=%d writes=%d",
							missing, blocked, jobs, attempts, f.attempts.Load(), f.writes.Load())
					}
				})
			}
		}
	}
}
