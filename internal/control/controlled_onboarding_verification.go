package control

import "context"

// Only a live actor-owned test scope can replace the onboarding slider. The
// mutation still repeats its existing scope and final submission checks.
func (s *GroupBindingService) ControlledVerificationExempt(ctx context.Context, ref SessionRef, in GroupBindingCreate, challengeID string) (bool, error) {
	if challengeID == "" && in.ControlledScopeID == "" {
		return false, nil
	}
	if challengeID != "" && !ValidID(challengeID) {
		return false, Denied(404, "group_binding_challenge_not_found")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	a, err := s.controlledOwner(ctx, tx, ref)
	if err != nil {
		return false, err
	}
	scopeID := in.ControlledScopeID
	if challengeID != "" {
		c, err := loadBindingRecord(ctx, tx, challengeID, false)
		if err != nil {
			return false, err
		}
		if _, err = s.checkRecord(ctx, tx, ref, c, false); err != nil {
			return false, err
		}
		if c.ControlledScope == "" {
			return false, nil
		}
		if err = requireControlledChallenge(ctx, tx, c); err != nil {
			return false, err
		}
		scopeID = c.ControlledScope
	} else if !ValidID(scopeID) || !bindingPlatform(in.Platform) || in.Platform != ref.assistantPlatform() {
		return false, Denied(400, "group_binding_request_invalid")
	}
	scope, err := s.readControlledScope(ctx, tx, a.ID, ref.assistantPlatform(), scopeID)
	if err != nil {
		return false, err
	}
	space, err := OperatorWorkspace(ctx, tx, a.ID)
	if err != nil {
		return false, err
	}
	if scope.TenantID != space.ID || challengeID == "" &&
		(in.TargetTenantID != "" && in.TargetTenantID != space.ID || in.RestoreGroupID != "") {
		return false, Denied(403, "controlled_onboarding_own_workspace_required")
	}
	if scope.HumanVerificationRequired {
		return false, Denied(409, "controlled_onboarding_scope_inactive")
	}
	return true, nil
}
