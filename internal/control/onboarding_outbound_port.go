package control

import (
	"context"
	"time"
)

// Outgoing requests establish contact only. The authenticated private-message
// proof remains the identity source used by group onboarding.
type GroupBindingFriendTarget struct {
	NativeID      string `json:"native_id"`
	AddAccount    string `json:"add_account"`
	Name          string `json:"name"`
	AlreadyFriend bool   `json:"already_friend"`
}

type GroupBindingOutboundEvidence struct {
	GroupBindingConnectionEvidence
	Target        GroupBindingFriendTarget
	SelfID        string
	ApplicationID string
	State         string // available, pending, friends, rejected, missing
	Peer          *GroupBindingOutboundPeer
}

type GroupBindingOutboundLookup struct {
	GroupBindingNativeScope
	AddAccount     string
	ControlledPeer *GroupBindingOutboundPeer
}

type GroupBindingOutboundPeer struct {
	GroupBindingNativeScope
	NativeID   string
	Connection GroupBindingConnectionEvidence
}

type GroupBindingOutboundRequest struct {
	GroupBindingNativeScope
	OperationID    string
	Target         GroupBindingFriendTarget
	SelfID         string
	ExpiresAt      time.Time
	ControlledPeer *GroupBindingOutboundPeer `json:",omitempty"`
}

func (GroupBindingOutboundRequest) String() string {
	return "GroupBindingOutboundRequest(redacted)"
}
func (GroupBindingOutboundRequest) GoString() string {
	return "GroupBindingOutboundRequest(redacted)"
}

type GroupBindingOutboundFence func(context.Context, GroupBindingOutboundEvidence, func(context.Context) error) error

type GroupBindingOutboundLookupFence func(context.Context, func(context.Context) error) error

type GroupBindingOutboundNativePort interface {
	LookupBindingFriend(context.Context, GroupBindingOutboundLookup, GroupBindingOutboundLookupFence) (GroupBindingOutboundEvidence, error)
	SendBindingFriend(context.Context, GroupBindingOutboundRequest, GroupBindingOutboundFence) error
	ReconcileBindingOutbound(context.Context, GroupBindingOutboundRequest) (GroupBindingOutboundEvidence, error)
}

type GroupBindingOutbound struct {
	ID                string                                `json:"id"`
	Status            string                                `json:"status"`
	Code              string                                `json:"code"`
	Version           int64                                 `json:"version"`
	Target            GroupBindingFriendTarget              `json:"target"`
	CanSend           bool                                  `json:"can_send"`
	SendBlockedReason string                                `json:"send_blocked_reason,omitempty"`
	CanReconcile      bool                                  `json:"can_reconcile"`
	SubmitDiagnostic  *GroupBindingOutboundSubmitDiagnostic `json:"submit_diagnostic,omitempty"`
}

type GroupBindingOutboundSubmitDiagnostic struct {
	Version      int       `json:"version"`
	Platform     string    `json:"platform"`
	Stage        string    `json:"stage"`
	Category     string    `json:"category"`
	ProviderCode int       `json:"provider_code"`
	ObservedAt   time.Time `json:"observed_at"`
}

type GroupBindingFriendTargetInput struct {
	Version int64  `json:"version"`
	Account string `json:"account"`
	Key     string `json:"-"`
}
