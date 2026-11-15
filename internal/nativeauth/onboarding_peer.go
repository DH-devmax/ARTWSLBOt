package nativeauth

import (
	"context"
	"strconv"
)

// RestrictOnboardingPeer pins a mapping already verified from the peer's sealed
// authenticated session. It does not establish website identity or permission.
// The caller owns that evidence; this client never performs public searches.
func (c *Client) RestrictOnboardingPeer(id, account string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.mobile || len(c.fields) <= 3 || !settingID(id) || !ValidOnboardingOutboundAccount(account) ||
		id == strconv.FormatUint(c.fields[3], 10) || c.onboardingPeer != nil {
		return rejection("native_onboarding_outbound_identity")
	}
	c.onboardingPeer = &OnboardingOutboundTarget{ID: id, Account: account}
	return nil
}

func (c *Client) lookupAuthenticatedOnboardingPeer(ctx context.Context, account string) (OnboardingOutboundTarget, error) {
	target := *c.onboardingPeer
	if account != target.Account {
		return OnboardingOutboundTarget{}, rejection("native_onboarding_outbound_changed")
	}
	uid, _ := strconv.ParseUint(target.ID, 10, 30)
	data, err := c.request(ctx, onboardingOutboundInfoRoute, map[string]any{
		"uid": uid, "showTempChat": false,
	}, nil)
	if err != nil {
		return OnboardingOutboundTarget{}, &OnboardingLookupError{Stage: "friend_info", Cause: err}
	}
	defer clearOnboardingOutboundData(data)
	// The sealed peer session already pins both identifiers. Desktop details
	// may omit the public account with its protobuf default, but never the UID.
	target, err = parseOnboardingOutboundInfo(data, target, "CONVERSATION_RELATION_STRANGER_USER", true)
	if err != nil {
		return OnboardingOutboundTarget{}, onboardingLookupError("friend_info", err)
	}
	return target, nil
}
