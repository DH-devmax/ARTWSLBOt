package gateway

import (
	"context"
	"time"
	"wangshangliao/internal/platform"
)

// Connector contracts describe evidence, not permission. A native directory or
// transport ACK never grants tenant ownership or authorizes an action.
type NativeScope struct {
	Platform            platform.ID
	AccountID, NativeID string
	Epoch               int64
}
type RoleEvidence struct {
	Scope                   NativeScope
	GroupID, MemberID, Role string
	ObservedAt              time.Time
	Verified                bool
}
type NativeCapability struct {
	ID, Implementation, Acceptance string
}
type TextReceipt struct {
	ClientID, ServerID, State string
	ObservedAt                time.Time
}
type SubmissionFence func(context.Context, func(context.Context) error) error
type TextConnector interface {
	Scope() NativeScope
	Capabilities() []NativeCapability
	SendText(context.Context, string, string, string, SubmissionFence) (TextReceipt, error)
	Close()
}
