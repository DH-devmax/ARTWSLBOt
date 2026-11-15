package control

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/scopecache"
)

type HeatChatSettings struct {
	Enabled          bool    `json:"enabled"`
	ReplyProbability float64 `json:"reply_probability"`
	CooldownSeconds  int     `json:"cooldown_seconds"`
	StartHour        int     `json:"start_hour"`
	EndHour          int     `json:"end_hour"`
}

type HeatEffectiveSettings struct {
	Enabled          bool    `json:"enabled"`
	ReplyProbability float64 `json:"reply_probability"`
	CooldownSeconds  int     `json:"cooldown_seconds"`
	AccountStartHour int     `json:"account_start_hour"`
	AccountEndHour   int     `json:"account_end_hour"`
	GroupStartMinute int     `json:"group_start_minute"`
	GroupEndMinute   int     `json:"group_end_minute"`
	PersonaSource    string  `json:"persona_source"`
	ModelAlias       string  `json:"model_alias"`
}

type HeatScope struct {
	controlledExecutionFence
	Service                                                          WorkspaceServiceContext
	Platform, Tenant, Actor, Group, NativeGroup, Account, NativeSelf string
	BindingVersion, BindingEpoch, AccountEpoch, AccountVersion       int64
	SettingsVersion, PreferenceVersion, PersonaVersion, RouteVersion int64
	ReceiverVersion                                                  int64
	ReceiverOwner                                                    string
	PersonaAccount                                                   string
	AdmissionVersion                                                 int64
	RuntimeVersion                                                   int64
	Effective                                                        HeatEffectiveSettings
	Persona, ReplyStyle                                              string `json:"-"`
}

func (s HeatScope) digest() []byte {
	raw, _ := json.Marshal(s)
	sum := sha256.Sum256(raw)
	return sum[:]
}

func (s HeatEffectiveSettings) allows(at time.Time) bool {
	local := at.In(time.FixedZone("Asia/Shanghai", 8*3600))
	hour, minute := local.Hour(), local.Hour()*60+local.Minute()
	account := hour >= s.AccountStartHour && hour < s.AccountEndHour
	group := minute >= s.GroupStartMinute && minute <= s.GroupEndMinute
	if s.GroupStartMinute > s.GroupEndMinute {
		group = minute >= s.GroupStartMinute || minute <= s.GroupEndMinute
	}
	return s.Enabled && account && group
}

func effectiveHeatSettings(account HeatChatSettings, preference bool, activity, start, end, cooldown int, persona string) HeatEffectiveSettings {
	out := HeatEffectiveSettings{Enabled: account.Enabled, ReplyProbability: account.ReplyProbability,
		CooldownSeconds:  max(5, account.CooldownSeconds, cooldown),
		AccountStartHour: account.StartHour, AccountEndHour: account.EndHour,
		GroupStartMinute: 0, GroupEndMinute: 1439, PersonaSource: persona, ModelAlias: "default"}
	if preference {
		out.ReplyProbability = float64(activity) / 100
		out.GroupStartMinute = start
		out.GroupEndMinute = end
	}
	return out
}

// heatScope is a live admission read. TTL storage contains only sealed settings;
// none of the authorization predicates is satisfied by a cached result.
func (s *HeatChatService) heatScope(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, group string, requireEnabled bool) (HeatScope, error) {
	var out HeatScope
	var settings []byte
	var preference bool
	var activity, start, end, cooldown int
	var published, admitted bool
	err := q.QueryRow(ctx, `SELECT g.platform,b.tenant_id::text,t.owner_id::text,g.id::text,g.native_id,
		a.id::text,a.native_uid,b.version,b.epoch,a.epoch,a.version,
		coalesce(ms.version,0),coalesce(cp.version,0),pr.version,pr.account_id::text,r.version,
		coalesce(cr.revision,0),coalesce(cr.owner,''),ha.version,ha.enabled,br.version,cp.group_id IS NOT NULL,
		coalesce(cp.activity,0),coalesce(cp.start_minute,0),coalesce(cp.end_minute,1439),
		coalesce((b.config->>'ai_cooldown_seconds')::int,0),pr.published,pr.personality,pr.reply_style
		FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
		JOIN tenants t ON t.id=b.tenant_id JOIN users u ON u.id=t.owner_id
		JOIN accounts a ON a.id=b.ai_account_id
		LEFT JOIN account_receivers cr ON cr.account_id=a.id
		LEFT JOIN maibot_settings ms ON ms.account_id=a.id AND ms.section='chat'
		LEFT JOIN group_chat_preferences cp ON cp.group_id=b.group_id AND cp.tenant_id=b.tenant_id
		JOIN personas pr ON pr.account_id=coalesce(cp.persona_id,a.id)
		JOIN accounts pa ON pa.id=pr.account_id
		LEFT JOIN subscriptions sub ON sub.tenant_id=b.tenant_id LEFT JOIN plans p ON p.id=sub.plan_id
		JOIN ai_model_routes r ON r.alias='default'
		JOIN heat_chat_admission ha ON ha.platform=g.platform
		JOIN group_business_runtime br ON br.group_id=b.group_id AND br.tenant_id=b.tenant_id
		WHERE b.group_id=$1 AND b.active AND b.ai_requested AND ($2=false OR b.ai_enabled)
		AND a.purpose='chat' AND a.platform=g.platform AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
		AND (NOT $2 OR cr.enabled AND cr.owner<>'' AND cr.revision>0
			AND cr.lease_until>clock_timestamp()+interval '3 seconds'
			AND cr.observed_at>clock_timestamp()-interval '15 seconds' AND cr.observed_at<=clock_timestamp()
			AND cr.snapshot->>'state'='connected')
		AND pa.platform=g.platform AND pa.purpose='chat'
		AND (workspace_is_demo(t.id,u.id) OR t.space_kind='tenant_business'
			AND u.role='user' AND u.tenant_id=b.tenant_id AND u.tenant_role='owner' AND NOT u.disabled
			AND sub.enabled AND sub.expires_at>clock_timestamp() AND 'ai'=ANY(p.modules)
			AND p.ai_group_limit>0 AND g.platform=ANY(p.allowed_platforms))
		AND (NOT $2 OR br.enabled) AND r.enabled`, group, requireEnabled).Scan(
		&out.Platform, &out.Tenant, &out.Actor, &out.Group, &out.NativeGroup, &out.Account, &out.NativeSelf,
		&out.BindingVersion, &out.BindingEpoch, &out.AccountEpoch, &out.AccountVersion,
		&out.SettingsVersion, &out.PreferenceVersion, &out.PersonaVersion, &out.PersonaAccount, &out.RouteVersion,
		&out.ReceiverVersion, &out.ReceiverOwner,
		&out.AdmissionVersion, &admitted, &out.RuntimeVersion, &preference, &activity, &start, &end, &cooldown, &published, &out.Persona, &out.ReplyStyle)
	if err != nil {
		return out, Denied(409, "heat_chat_scope_unavailable")
	}
	out.controlledExecutionFence, err = controlledExecutionScope(ctx, q, "", out.Platform, group)
	if err != nil {
		return out, err
	}
	out.Service, err = WorkspaceServiceEntitlement(ctx, q, out.Tenant)
	if err != nil || out.Service.OwnerID != out.Actor {
		return out, Denied(409, "heat_chat_scope_unavailable")
	}
	if requireEnabled && !admitted {
		return out, Denied(409, "heat_chat_platform_not_admitted")
	}
	if requireEnabled {
		if err = RequireBusinessAdmission(ctx, q, out.Platform, group, "send_text", "heatbot"); err != nil {
			return out, err
		}
	}
	if out.PersonaAccount != out.Account && !published {
		return out, Denied(409, "heat_chat_persona_unavailable")
	}
	account := HeatChatSettings{ReplyProbability: 0.1, CooldownSeconds: 30, EndHour: 24}
	if out.SettingsVersion > 0 {
		key := scopecache.Key{Platform: out.Platform, Purpose: "chat", Account: out.Account, Group: out.Group,
			Tenant: out.Tenant, Actor: out.Actor, Kind: "sealed_config", Owner: "control:" + out.ReceiverOwner,
			AccountEpoch: out.AccountEpoch, AccountVersion: out.AccountVersion, BindingVersion: out.BindingVersion,
			ConfigVersion: out.SettingsVersion, ReceiverVersion: out.ReceiverVersion}
		var settingsQuery Queryer = q
		if s.Cache != nil {
			settingsQuery = s.DB.Platform
		}
		fetch := func(work context.Context) ([]byte, error) {
			var sealed []byte
			e := settingsQuery.QueryRow(work, `SELECT sealed FROM maibot_settings
				WHERE account_id=$1 AND section='chat' AND version=$2`, out.Account, out.SettingsVersion).Scan(&sealed)
			return sealed, e
		}
		if s.Cache == nil {
			settings, err = fetch(ctx)
		} else {
			settings, _, err = s.Cache.Load(ctx, key, fetch)
		}
		if err != nil {
			return out, err
		}
		raw, e := s.Vault.Open("maibot:"+out.Account+":chat", settings)
		if e != nil {
			return out, Denied(503, "heat_chat_settings_unreadable")
		}
		e = json.Unmarshal(raw, &account)
		clear(raw)
		if e != nil {
			return out, Denied(503, "heat_chat_settings_unreadable")
		}
	}
	if math.IsNaN(account.ReplyProbability) || account.ReplyProbability < 0 || account.ReplyProbability > 1 ||
		account.CooldownSeconds < 5 || account.CooldownSeconds > 3600 || account.StartHour < 0 ||
		account.StartHour >= account.EndHour || account.EndHour > 24 {
		return out, Denied(409, "heat_chat_settings_invalid")
	}
	out.Effective = effectiveHeatSettings(account, preference, activity, start, end, cooldown, out.PersonaAccount)
	if activity < 0 || activity > 100 || start < 0 || start > 1439 || end < 0 || end > 1439 {
		return out, Denied(409, "heat_chat_settings_invalid")
	}
	return out, nil
}
