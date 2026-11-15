package nativewire

import "time"

// RoleEvidence crosses the internal connection-owner boundary only. It is not
// a website input, a tenant grant, or proof that a message was delivered.
type RoleEvidence struct {
	AccountID, ReceiverOwner, NativeGroup, Name, Team, Sender, Role string
	AccountEpoch, ReceiverRevision, DeploymentVersion               int64
	UID                                                             uint64
	ObservedAt                                                      time.Time
	IndividualMuted                                                 bool
}
