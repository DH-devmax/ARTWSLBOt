package control

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"wangshangliao/internal/features"
)

// SuppressesHeat shares the pure rule decision without creating an automatic
// task or submitting an action from the chat pipeline.
func (s *BusinessRulesService) SuppressesHeat(ctx context.Context, chat HeatScope, message HeatMessage) (bool, error) {
	if businessChallengeText(message.Text) {
		return true, nil
	}
	if !ValidID(chat.Group) || !ValidID(chat.Tenant) || chat.Platform == "" || chat.NativeGroup == "" ||
		message.Group != chat.NativeGroup {
		return false, nil
	}
	var raw []byte
	var bindingVersion, bindingEpoch, runtimeVersion int64
	err := s.DB.Platform.QueryRow(ctx, `SELECT b.config,b.version,b.epoch,r.version FROM group_business_runtime r
		JOIN group_bindings b ON b.group_id=r.group_id AND b.tenant_id=r.tenant_id
		JOIN native_groups g ON g.id=b.group_id
		WHERE r.group_id=$1 AND r.tenant_id=$2 AND g.platform=$3
		AND r.enabled AND b.active AND b.management_enabled`,
		chat.Group, chat.Tenant, chat.Platform).Scan(&raw, &bindingVersion, &bindingEpoch, &runtimeVersion)
	// Unavailable management rules cannot interrupt independently admitted chat.
	if err != nil {
		return false, nil
	}
	var config features.Config
	if len(raw) > 1<<20 || json.Unmarshal(raw, &config) != nil || features.Validate(config) != nil {
		return false, nil
	}
	input := features.BusinessRuleInput{Sender: message.Sender, Text: message.Text}
	if !features.BusinessRuleCandidate(config, input) {
		return false, nil
	}
	scope, config, err := s.scope(ctx, s.DB.Platform, chat.Group)
	if err != nil || scope.Platform != chat.Platform || scope.Tenant != chat.Tenant ||
		scope.NativeGroup != chat.NativeGroup || scope.BindingVersion != bindingVersion ||
		scope.BindingEpoch != bindingEpoch || scope.RuntimeVersion != runtimeVersion {
		return false, nil
	}
	if s.FreshRole == nil {
		return false, nil
	}
	proof, err := s.FreshRole(ctx, scope, message.Sender)
	if err != nil {
		return false, nil
	}
	decision := businessHeatDecision(scope, config, message, proof, time.Now())
	if !decision.SuppressHeat {
		return false, nil
	}
	if err = s.authorizeAction(ctx, s.DB.Platform, scope, config, decision.Rule, decision.Action); err != nil {
		return false, nil
	}
	if err = requireBusinessOwnerAnchor(ctx, s.DB.Platform, scope, proof.OwnerID); err != nil {
		return false, nil
	}
	current, _, err := s.scope(ctx, s.DB.Platform, chat.Group)
	if err != nil || !bytes.Equal(scope.snapshot(), current.snapshot()) {
		return false, nil
	}
	return decision.SuppressHeat, nil
}

func businessHeatDecision(scope BusinessScope, config features.Config, message HeatMessage,
	proof BusinessEvidence, now time.Time) features.BusinessDecision {
	if message.Group != scope.NativeGroup || message.Sender == scope.NativeSelf ||
		validateBusinessRole(scope, message.Sender, proof, now) != nil {
		return features.BusinessDecision{}
	}
	decision := features.EvaluateBusinessRules(config, features.BusinessRuleInput{
		Sender: message.Sender, Text: message.Text, Role: proof.TargetRole,
	})
	if decision.Action == "" || (decision.Action == features.BusinessRecall || decision.Action == features.BusinessMute) &&
		proof.SelfRole != "admin" && proof.SelfRole != "owner" {
		return features.BusinessDecision{}
	}
	return decision
}
