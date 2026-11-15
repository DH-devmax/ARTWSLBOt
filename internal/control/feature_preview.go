package control

import (
	"context"
	"encoding/json"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/features"
)

// Preview input describes a simulation, never native identity or role evidence.
type FeaturePreviewInput struct {
	Version int64          `json:"version"`
	Feature string         `json:"feature"`
	Changes FeatureChanges `json:"changes"`
	Text    string         `json:"text"`
	Sender  string         `json:"sender"`
	Role    string         `json:"role"`
}

type FeaturePreview struct {
	Group           string                    `json:"group_id"`
	Platform        string                    `json:"platform"`
	Version         int64                     `json:"version"`
	Feature         string                    `json:"feature"`
	ValidationLevel string                    `json:"validation_level"`
	Applied         bool                      `json:"applied"`
	NativeSubmitted bool                      `json:"native_submitted"`
	Decision        features.BusinessDecision `json:"decision"`
	CooldownSeconds int                       `json:"cooldown_seconds"`
	SampledAt       time.Time                 `json:"sampled_at"`
}

func ValidateFeaturePreview(in FeaturePreviewInput) error {
	if in.Version < 1 || !slices.Contains([]string{"blocked_words", "blacklist", "exemptions", "keyword_replies"}, in.Feature) ||
		len(in.Text) == 0 || len(in.Text) > 16384 || !utf8.ValidString(in.Text) ||
		len(in.Sender) > 200 || !utf8.ValidString(in.Sender) ||
		!slices.Contains([]string{"member", "admin", "owner"}, in.Role) ||
		len(in.Changes) > 1 {
		return Denied(400, "invalid_feature_preview")
	}
	for id := range in.Changes {
		if id != in.Feature {
			return Denied(400, "invalid_feature_preview")
		}
	}
	return nil
}

// PreviewFeaturesTx shares configuration validation without invoking a save,
// audit write, native port or business consumer. Existing entitlement and
// ownership checks may lock rows, but this path never modifies them.
func PreviewFeaturesTx(ctx context.Context, tx pgx.Tx, actor Actor, catalog []PermissionSpec,
	platform, group string, in FeaturePreviewInput) (FeaturePreview, error) {
	out := FeaturePreview{Group: group, Platform: platform, Version: in.Version, Feature: in.Feature,
		ValidationLevel: "simulation", SampledAt: time.Now().UTC()}
	if err := ValidateFeaturePreview(in); err != nil {
		return out, err
	}
	if err := RequireHumanGroupOwnership(ctx, tx, actor, platform, group); err != nil {
		return out, err
	}
	entitlement, err := ResolveHumanEntitlement(ctx, tx, actor, catalog, platform, group)
	if err != nil {
		return out, err
	}
	grants, err := EffectivePermissions(ctx, tx, actor, catalog)
	if err != nil {
		return out, err
	}
	definition, _ := features.Find(in.Feature)
	if len(in.Changes) != 0 && (!entitlement.Active || !slices.Contains(entitlement.Modules, definition.Module)) {
		return out, Denied(403, "module_not_in_plan")
	}
	if actor.Role != "superadmin" && !slices.Contains(grants, "groups:read") {
		return out, Denied(403, "permission_denied")
	}
	var raw []byte
	var version int64
	err = tx.QueryRow(ctx, `SELECT b.config,b.version FROM group_bindings b
		JOIN native_groups g ON g.id=b.group_id WHERE b.group_id=$1 AND b.active AND g.platform=$2`,
		group, platform).Scan(&raw, &version)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "group_not_found")
	}
	if err != nil {
		return out, err
	}
	if version != in.Version {
		return out, Denied(409, "version_conflict")
	}
	var config features.Config
	if len(raw) > 1<<20 || json.Unmarshal(raw, &config) != nil || features.Validate(config) != nil {
		return out, Denied(409, "business_configuration_invalid")
	}
	if len(in.Changes) != 0 {
		if actor.Role != "superadmin" {
			if err = requireFeatureCeilings(ctx, tx, actor.TenantID, in.Changes); err != nil {
				return out, err
			}
		}
		config, _, err = ValidateFeatures(config, in.Changes, actor, grants, entitlement.Modules, false)
		if err != nil {
			return out, err
		}
	}
	sender := in.Sender
	if sender == "" {
		sender = "preview-member"
	}
	out.Decision = features.EvaluateBusinessRules(config, features.BusinessRuleInput{
		Sender: sender, Text: in.Text, Role: in.Role, Challenge: businessChallengeText(in.Text),
	})
	out.CooldownSeconds = features.KeywordReplyCooldown(config)
	return out, nil
}
