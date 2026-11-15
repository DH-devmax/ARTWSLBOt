package control

import (
	"context"
	"time"
)

// GroupBindingNativePort is implemented by the authenticated connection owner.
// No HTTP handler may construct its evidence from browser-provided identity,
// role, message body or invitation payload.
type GroupBindingNativePort interface {
	BindingRoles(context.Context, GroupBindingNativeScope) (GroupBindingRoleEvidence, error)
	BindingInvitation(context.Context, GroupBindingInvitationRequest) (GroupBindingInvitationEvidence, error)
	AcceptBindingInvitation(context.Context, GroupBindingAcceptRequest, GroupBindingAcceptFence) error
}

type GroupBindingNativeScope struct {
	Platform       string
	AccountID      string
	AccountEpoch   int64
	AccountVersion int64
	NativeGroupID  string
	NativeSenderID string
}

// Decode receives only sealed projections and their sealed authenticated source,
// read by control from durable ingress. It must reject quote/@/media, outbound,
// replay and unsupported sessions. Plain text is limited to 160 bytes.
type OnboardingDecode func(platform, account, self, key string, event int64, projection, source []byte) (OnboardingMessage, error)

type OnboardingMessage struct {
	// Recipient is the native wire target: the robot in private sessions,
	// the native group in group sessions.
	Group, Sender, Recipient, Text, ClientID string
	Private                                  bool
}

type GroupBindingConnectionEvidence struct {
	Platform                string
	AccountID               string
	AccountEpoch            int64
	AccountVersion          int64
	ReceiverOwner           string
	ReceiverRevision        int64
	DeploymentVersion       int64
	MobileDeploymentVersion int64 `json:",omitempty"`
	MobileSessionVersion    int64 `json:",omitempty"`
	CredentialDigest        [32]byte
	ObservedAt              time.Time
}

type GroupBindingRoleEvidence struct {
	GroupBindingConnectionEvidence
	NativeGroupID  string
	NativeSenderID string
	NativeOwnerID  string
	SenderRole     string
	RobotRole      string
	Present        bool
	GroupName      string
	// Set only after a successful fresh directory read proves the robot absent.
	// A failed role/directory request must never set this field.
	AbsenceVerified bool
}

// InvitationID is only a lookup selector. The port resolves group, recipient and
// expiry from the current account's native invitation, not from request fields.
type GroupBindingInvitationRequest struct {
	GroupBindingNativeScope
	InvitationID string
}

type GroupBindingInvitationEvidence struct {
	GroupBindingConnectionEvidence
	InvitationID  string
	NativeGroupID string
	RecipientID   string
	InviterID     string
	GroupName     string
	// ExpiresAt is a local evidence deadline, not an upstream invitation TTL.
	// The port rereads the pending invitation before the final submission.
	ExpiresAt time.Time
}

type GroupBindingAcceptRequest struct {
	GroupBindingNativeScope
	OperationID  string
	InvitationID string
	RecipientID  string
	InviterID    string
	ExpiresAt    time.Time
}

// The port must call fence exactly once immediately before its native mutation.
// submit contains only the already-resolved invitation acceptance. It must not
// retry, log credentials, accept another group or create authorization.
type GroupBindingAcceptFence func(context.Context, GroupBindingInvitationEvidence, func(context.Context) error) error
