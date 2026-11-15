package control

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/features"
	"wangshangliao/internal/nativeauth"
)

func TestBusinessEvidenceFinalValidation(t *testing.T) {
	now := time.Now()
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			scope := BusinessScope{Platform: platform, Account: NewID(), Group: NewID(),
				NativeGroup: "100", NativeSelf: "101", AccountEpoch: 3, AccountVersion: 4, DeploymentVersion: 5}
			base := BusinessEvidence{Platform: platform, Account: scope.Account, Group: "100",
				SelfID: "101", Target: "102", OwnerID: "103", SelfRole: "admin", TargetRole: "member",
				AccountEpoch: 3, AccountVersion: 4, DeploymentVersion: 5, ObservedAt: now}
			config := features.Config{Blacklist: []string{"102"}, FeatureOptions: map[string]features.Options{
				"blacklist": {Enabled: true}, "blocked_words": {Enabled: true}, "keyword_replies": {Enabled: true},
			}}
			payload := businessPayload{Sender: "102", Decision: features.BusinessDecision{
				Rule: "blacklist", Action: features.BusinessMute, Target: "102", Minutes: 10}}
			if err := validateBusinessEvidence(scope, payload, config, base, now); err != nil {
				t.Fatal("valid synthetic evidence", err)
			}
			for _, tc := range []struct {
				name string
				edit func(*BusinessEvidence)
			}{
				{"other_platform", func(e *BusinessEvidence) { e.Platform = "other" }},
				{"other_account", func(e *BusinessEvidence) { e.Account = NewID() }},
				{"other_group", func(e *BusinessEvidence) { e.Group = "other" }},
				{"other_sender", func(e *BusinessEvidence) { e.Target = "other" }},
				{"other_self", func(e *BusinessEvidence) { e.SelfID = "other" }},
				{"old_epoch", func(e *BusinessEvidence) { e.AccountEpoch-- }},
				{"old_version", func(e *BusinessEvidence) { e.AccountVersion-- }},
				{"old_deployment", func(e *BusinessEvidence) { e.DeploymentVersion-- }},
				{"future_observation", func(e *BusinessEvidence) { e.ObservedAt = now.Add(time.Nanosecond) }},
				{"stale_observation", func(e *BusinessEvidence) { e.ObservedAt = now.Add(-6 * time.Second) }},
				{"no_observation", func(e *BusinessEvidence) { e.ObservedAt = time.Time{} }},
				{"unknown_target", func(e *BusinessEvidence) { e.TargetRole = "" }},
				{"unknown_self", func(e *BusinessEvidence) { e.SelfRole = "" }},
				{"target_admin", func(e *BusinessEvidence) { e.TargetRole = "admin" }},
				{"target_owner", func(e *BusinessEvidence) { e.TargetRole, e.OwnerID = "owner", e.Target }},
				{"self_member", func(e *BusinessEvidence) { e.SelfRole = "member" }},
				{"missing_owner", func(e *BusinessEvidence) { e.OwnerID = "" }},
				{"inconsistent_owner", func(e *BusinessEvidence) { e.OwnerID = e.Target }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					e := base
					tc.edit(&e)
					if validateBusinessEvidence(scope, payload, config, e, now) == nil {
						t.Fatal("invalid evidence authorized a mutation")
					}
				})
			}
			for _, minutes := range []int{-1, 0, 1, 9, 11, 600} {
				p := payload
				p.Decision.Minutes = minutes
				if validateBusinessEvidence(scope, p, config, base, now) == nil {
					t.Fatal("variable mute duration authorized", minutes)
				}
			}
			config.FeatureOptions["exemptions"] = features.Options{Enabled: true}
			config.Exemptions = []string{payload.Sender}
			if validateBusinessEvidence(scope, payload, config, base, now) == nil {
				t.Fatal("exempted member moderated")
			}
		})
	}
}

func TestBusinessEvidenceOriginalRecallAndReply(t *testing.T) {
	now := time.Now()
	scope := BusinessScope{Platform: "lajiaoliao", Account: NewID(), NativeGroup: "group",
		NativeSelf: "robot", AccountEpoch: 1, AccountVersion: 1}
	proof := BusinessEvidence{Platform: scope.Platform, Account: scope.Account, Group: scope.NativeGroup,
		SelfID: scope.NativeSelf, Target: "human", OwnerID: "owner", SelfRole: "admin",
		TargetRole: "member", AccountEpoch: 1, AccountVersion: 1, ObservedAt: now}
	config := features.Config{FeatureOptions: map[string]features.Options{
		"blocked_words": {Enabled: true}, "keyword_replies": {Enabled: true},
	}}
	p := businessPayload{Sender: "human", Decision: features.BusinessDecision{
		Action: features.BusinessRecall, Rule: "blocked_words", Target: "human"},
		Reference: &nativeauth.MessageReference{Platform: scope.Platform, Group: scope.NativeGroup,
			Sender: "human", EventID: 1, CreatedAt: now.Add(-time.Second).UnixMilli()}}
	if err := validateBusinessEvidence(scope, p, config, proof, now); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*nativeauth.MessageReference){
		func(r *nativeauth.MessageReference) { r.Group = "other" },
		func(r *nativeauth.MessageReference) { r.Platform = "wsl" },
		func(r *nativeauth.MessageReference) { r.Sender = "other" },
		func(r *nativeauth.MessageReference) { r.EventID = 0 },
		func(r *nativeauth.MessageReference) { r.CreatedAt = now.Add(-2 * time.Minute).UnixMilli() },
		func(r *nativeauth.MessageReference) { r.CreatedAt = now.Add(time.Second).UnixMilli() },
	} {
		ref := *p.Reference
		edit(&ref)
		invalid := p
		invalid.Reference = &ref
		if validateBusinessEvidence(scope, invalid, config, proof, now) == nil {
			t.Fatal("changed original reference authorized")
		}
	}
	p.Reference = nil
	if validateBusinessEvidence(scope, p, config, proof, now) == nil {
		t.Fatal("missing original route authorized")
	}
	p.Decision = features.BusinessDecision{Rule: "keyword_replies", Action: features.BusinessReply, Text: "one"}
	proof.SelfRole = "member"
	if err := validateBusinessEvidence(scope, p, config, proof, now); err != nil {
		t.Fatal("ordinary robot could not send a permitted keyword reply", err)
	}
	for _, reply := range []string{"", strings.Repeat("x", 4001), string([]byte{0xff})} {
		p.Decision.Text = reply
		if validateBusinessEvidence(scope, p, config, proof, now) == nil {
			t.Fatal("invalid single reply authorized")
		}
	}
}

func TestBusinessScopeSnapshotIsolation(t *testing.T) {
	original := BusinessScope{Platform: "wsl", Group: NewID(), Account: NewID(), Tenant: NewID(),
		Actor: NewID(), AccountEpoch: 1, BindingVersion: 2, RuntimeVersion: 3,
		Authorization: []string{"group_action:send_text"}, AdmissionVersions: []string{"send_text:1:pilot:true"}}
	for _, edit := range []func(*BusinessScope){
		func(s *BusinessScope) { s.Platform = "lajiaoliao" },
		func(s *BusinessScope) { s.Tenant = NewID() },
		func(s *BusinessScope) { s.Actor = NewID() },
		func(s *BusinessScope) { s.Group = NewID() },
		func(s *BusinessScope) { s.Account = NewID() },
		func(s *BusinessScope) { s.AccountEpoch++ },
		func(s *BusinessScope) { s.BindingVersion++ },
		func(s *BusinessScope) { s.RuntimeVersion++ },
		func(s *BusinessScope) { s.Authorization = nil },
		func(s *BusinessScope) { s.AdmissionVersions = []string{"send_text:2:closed:false"} },
	} {
		changed := original
		edit(&changed)
		if bytes.Equal(changed.snapshot(), original.snapshot()) {
			t.Fatal("scope change did not cancel the immutable job")
		}
	}
}

func TestBusinessChallengesExcludedWithoutNativeRead(t *testing.T) {
	token := strings.Repeat("a", 43)
	for _, text := range []string{"群绑定 " + token, "群接入 " + token} {
		if !businessChallengeText(text) {
			t.Fatal("onboarding challenge entered a business consumer")
		}
	}
	for _, text := range []string{"群绑定", "群绑定 " + token + "\n", "hello 群绑定 " + token,
		"群接入 " + token[:42], "群绑定 " + strings.Repeat("!", 43)} {
		if businessChallengeText(text) {
			t.Fatal("ordinary text was mistaken for a challenge")
		}
	}
}

func TestBusinessHeatDecisionRequiresFreshRoles(t *testing.T) {
	now := time.Now()
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			scope := BusinessScope{Platform: platform, Account: NewID(), NativeGroup: "group",
				NativeSelf: "robot", AccountEpoch: 1, AccountVersion: 2, DeploymentVersion: 3}
			config := features.Config{BlockedWords: []string{"bad"}, Exemptions: []string{"exempt"},
				KeywordReplies: []features.KeywordRule{{Keyword: "hello", Reply: "one reply"}},
				FeatureOptions: map[string]features.Options{
					"blocked_words": {Enabled: true}, "exemptions": {Enabled: true},
					"keyword_replies": {Enabled: true, AdminOnly: true},
				}}
			base := BusinessEvidence{Platform: platform, Account: scope.Account, Group: scope.NativeGroup,
				SelfID: scope.NativeSelf, Target: "human", OwnerID: "owner", SelfRole: "admin",
				TargetRole: "member", AccountEpoch: 1, AccountVersion: 2, DeploymentVersion: 3, ObservedAt: now}
			for _, tc := range []struct {
				name   string
				sender string
				text   string
				edit   func(*BusinessEvidence)
				want   bool
			}{
				{"ordinary_recall", "human", "bad", nil, true},
				{"unknown_target", "human", "bad", func(p *BusinessEvidence) { p.TargetRole = "" }, false},
				{"unknown_self", "human", "bad", func(p *BusinessEvidence) { p.SelfRole = "" }, false},
				{"ordinary_robot", "human", "bad", func(p *BusinessEvidence) { p.SelfRole = "member" }, false},
				{"native_admin_exempt", "human", "bad", func(p *BusinessEvidence) { p.TargetRole = "admin" }, false},
				{"native_owner_exempt", "human", "bad", func(p *BusinessEvidence) {
					p.TargetRole, p.OwnerID = "owner", p.Target
				}, false},
				{"explicit_exempt", "exempt", "bad", nil, false},
				{"admin_only_denied", "human", "hello", nil, false},
				{"admin_only_allowed", "human", "hello", func(p *BusinessEvidence) { p.TargetRole = "admin" }, true},
				{"stale", "human", "bad", func(p *BusinessEvidence) { p.ObservedAt = now.Add(-6 * time.Second) }, false},
				{"future", "human", "bad", func(p *BusinessEvidence) { p.ObservedAt = now.Add(time.Second) }, false},
				{"old_epoch", "human", "bad", func(p *BusinessEvidence) { p.AccountEpoch-- }, false},
				{"wrong_platform", "human", "bad", func(p *BusinessEvidence) { p.Platform = "other" }, false},
				{"wrong_target", "human", "bad", func(p *BusinessEvidence) { p.Target = "other" }, false},
				{"owner_inconsistent", "human", "bad", func(p *BusinessEvidence) { p.OwnerID = p.Target }, false},
				{"nonmatch", "human", "ordinary message", nil, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					proof := base
					proof.Target = tc.sender
					if tc.edit != nil {
						tc.edit(&proof)
					}
					decision := businessHeatDecision(scope, config, HeatMessage{
						Group: scope.NativeGroup, Sender: tc.sender, Text: tc.text,
					}, proof, now)
					if decision.SuppressHeat != tc.want {
						t.Fatal("unverified or exempt role suppressed heat", decision)
					}
				})
			}
		})
	}
}
