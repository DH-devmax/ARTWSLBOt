package nativewire

import "time"

// TextRoute is transport evidence, not a website grant or a browser input.
// Digest identifies the exact sealed attachment; it contains no message text.
type TextRoute struct {
	Team, Sender      string
	PrivatePeer       string
	DeploymentVersion int64
	Digest            []byte
	ObservedAt        time.Time
}

type TextEvidenceQuery struct {
	AccountID, ClientID string
	AccountEpoch        int64
	Route               TextRoute
	SubmittedAt         time.Time
}

type TextEvidence struct {
	Found, Truncated bool
	EventID          int64
	Receipt          SendReceipt
}
