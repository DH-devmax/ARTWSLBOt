package control

import (
	"context"
	"time"
)

// GroupBindingFriendNativePort is optional. It is available only to explicit
// onboarding setup, preparation, approval and reconciliation, never page reads.
type GroupBindingFriendNativePort interface {
	BindingFriend(context.Context, GroupBindingFriendRequest) (GroupBindingFriendEvidence, error)
	ApproveBindingFriend(context.Context, GroupBindingFriendApproval, GroupBindingFriendFence) error
	ReconcileBindingFriend(context.Context, GroupBindingFriendApproval) (GroupBindingFriendEvidence, error)
	BindingRobot(context.Context, GroupBindingNativeScope) (GroupBindingRobot, error)
}

// Expired-request cleanup has no identity-proof effect and cannot approve a
// contact. It reuses the original sealed attempt and the same final fence.
type GroupBindingExpiredFriendNativePort interface {
	RejectBindingFriend(context.Context, GroupBindingFriendApproval, GroupBindingFriendFence) error
}

// ChallengeText must be stored in the control-owned sealed payload, separately
// from this safe JSON projection. Native adapters compare it locally.
type GroupBindingFriendRequest struct {
	GroupBindingNativeScope
	ChallengeText string                      `json:"-"`
	Contact       *GroupBindingFriendSelector `json:"-"`
}

// Contact selection never proves website identity. Its bounds are generated
// from the persisted challenge, not accepted from the browser.
type GroupBindingFriendSelector struct {
	Name          string
	Since, Until  time.Time
	RejectExpired bool
}

func (GroupBindingFriendRequest) String() string   { return "GroupBindingFriendRequest(redacted)" }
func (GroupBindingFriendRequest) GoString() string { return "GroupBindingFriendRequest(redacted)" }

type GroupBindingFriendEvidence struct {
	GroupBindingConnectionEvidence
	ApplicationID string
	SenderID      string
	RecipientID   string
	// pending, approved, rejected, expired, deleted or unknown.
	State       string
	Name        string
	RequestedAt time.Time
	// Local evidence deadline; not an upstream application TTL.
	ExpiresAt time.Time
}

type GroupBindingFriendApproval struct {
	GroupBindingFriendRequest
	OperationID   string
	ApplicationID string
	SenderID      string
	RecipientID   string
	RequestedAt   time.Time
	ExpiresAt     time.Time
	RejectExpired bool
}

// The adapter rereads the pending application before invoking the final native
// fence exactly once. Unknown results may only be reconciled, never replayed.
type GroupBindingFriendFence func(context.Context, GroupBindingFriendEvidence, func(context.Context) error) error
