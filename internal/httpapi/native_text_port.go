package httpapi

import (
	"context"
	"errors"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativewire"
)

// Internal composition for the separately authorized superadmin test surface.
// Production operation admission remains independently closed.
type nativeTextPort struct{ receiver *gateway.ReceiverManager }

var _ control.TextPort = nativeTextPort{}

func (p nativeTextPort) SubmitText(ctx context.Context, d control.TextDispatch, fence nativewire.SubmissionFence) (nativewire.TextSendResult, error) {
	if p.receiver == nil {
		return nativewire.TextSendResult{}, errors.New("native_text_transport_unavailable")
	}
	return p.receiver.SubmitText(ctx, gateway.TextIntent{AccountID: d.AccountID, AccountEpoch: d.AccountEpoch,
		ReceiverOwner: d.ReceiverOwner, Revision: d.ReceiverRevision, Message: d.Message,
		PrivatePeer: d.PrivatePeer, PrivateConversation: d.PrivateConversation, PrivateEventID: d.PrivateEventID,
		QuoteEventID: d.QuoteEventID, Nonce: d.Nonce, AAD: d.AAD, RecordRoute: d.RecordRoute}, fence)
}
