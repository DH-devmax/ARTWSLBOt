package control

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *GroupBindingService) outboundFriendWriteAvailable(ctx context.Context, tx pgx.Tx,
	c bindingRecord, record bindingOutboundRecord, payload bindingOutboundPayload, reserved *time.Time) error {
	retired := ""
	a := payload.Controlled
	if c.Platform == "lajiaoliao" && a != nil && a.Claimant != nil {
		if !sameOnboardingControlled(a, onboardingControlledContext(ctx)) ||
			!a.Claimant.matches(c, a.Group, payload.Request.Target.NativeID) ||
			payload.Request.AccountID != a.Claimant.Account ||
			payload.Request.SelfID == "" || payload.Request.Target.NativeID != a.Claimant.Recipient {
			return Denied(409, "onboarding_controlled_target_changed")
		}
		if _, err := s.verifyOnboardingControlled(ctx, tx, c, a, true, record.Used == 1); err != nil {
			return err
		}
		var err error
		retired, err = retiredChiliPrivateProof(ctx, tx, s.Vault, retiredChiliProofContext{
			Actor: c.Applicant, Tenant: c.Tenant, Account: a.Claimant.Account, Group: a.Group,
			NativeSender: payload.Request.SelfID, NativeGroup: a.NativeGroup, Recipient: a.Target,
			Challenge: c.ID, Scope: c.ControlledScope, Window: a.Window,
		}, "")
		if err != nil {
			return err
		}
	}
	return bindingFriendWriteAvailableExcept(ctx, tx, record.Account, "", retired, reserved)
}
