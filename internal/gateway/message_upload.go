package gateway

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

type MediaUploadIntent struct {
	AccountID, ReceiverOwner, Operation string
	AccountEpoch, Revision              int64
	Sender, Group                       uint64
	PrivatePeer                         *nativewire.PrivateTarget
	PrivateConversation                 string
	PrivateEventID                      int64
	Input                               nativeauth.MessageUpload
}

type messageUploadClient interface {
	textReadClient
	UploadMessage(context.Context, nativeauth.MessageUpload, func(context.Context) error) (nativewire.OutboundMedia, error)
}

func (m *ReceiverManager) UploadMessage(ctx context.Context, intent MediaUploadIntent, guard func(context.Context) error) (nativewire.OutboundMedia, error) {
	return m.uploadMessage(ctx, intent, guard, func(d nativeauth.Deployment, session nativeauth.Session, ids nativeauth.RequestIDSource) (messageUploadClient, error) {
		return nativeauth.Resume(d, session, ids)
	})
}

// Uploads use the existing per-account serial business lane and global read
// admission. The socket reader, heartbeat, reliable ingress and ACK never wait.
func (m *ReceiverManager) uploadMessage(ctx context.Context, intent MediaUploadIntent, guard func(context.Context) error,
	factory func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (messageUploadClient, error)) (nativewire.OutboundMedia, error) {
	i := TextIntent{AccountID: intent.AccountID, ReceiverOwner: intent.ReceiverOwner,
		AccountEpoch: intent.AccountEpoch, Revision: intent.Revision, Message: nativewire.GroupText{SenderID: intent.Sender},
		PrivatePeer: intent.PrivatePeer, PrivateConversation: intent.PrivateConversation, PrivateEventID: intent.PrivateEventID}
	w, connection, err := m.textOwner(i)
	if err != nil || guard == nil || intent.Operation == "" || intent.Group == 0 ||
		intent.PrivatePeer != nil && intent.Input.Kind != "image" {
		return nativewire.OutboundMedia{}, ErrTextOwner
	}
	ctx, stop := context.WithCancel(ctx)
	follow := context.AfterFunc(connection.ctx, stop)
	defer func() { follow(); stop() }()
	value, err := m.readValue(ctx, intent.AccountID, fmt.Sprintf("message-upload:%s", intent.Operation), ReadRefresh,
		func(work context.Context) (any, error) {
			d, saved, _, _, err := m.load(work, w.spec)
			if err != nil || saved.DeploymentVersion != connection.version ||
				saved.Session.UID != connection.uid || saved.Session.NIMID != connection.sender {
				return nil, ErrTextOwner
			}
			var ids nativeauth.RequestIDSource
			if m.IDs != nil {
				ids = m.IDs(saved.DeploymentVersion)
			}
			client, err := factory(d, saved.Session, ids)
			if err != nil {
				return nil, err
			}
			defer client.Close()
			proof, err := proveNativeGroup(work, client, connection, strconv.FormatUint(intent.Group, 10))
			if err != nil || proof.muted ||
				verifyTextRoster(proof.roster, strconv.FormatUint(intent.Group, 10), connection.uid, connection.sender) != nil {
				return nil, ErrTextRoute
			}
			if intent.PrivatePeer != nil {
				keyText := d.MessageKey
				if keyText == "" && d.ProtocolVersion == nativeauth.DesktopProtocolVersion {
					keyText = d.BodyKey
				}
				key, err := base64.StdEncoding.Strict().DecodeString(keyText)
				if err != nil || len(key) != 32 {
					clear(key)
					return nil, ErrTextRoute
				}
				err = m.provePrivateTarget(work, client, i, connection, proof.roster, key)
				clear(key)
				if err != nil {
					return nil, err
				}
			}
			return client.UploadMessage(work, intent.Input, func(final context.Context) error {
				if _, current, err := m.textOwner(i); err != nil || current != connection ||
					time.Since(proof.at) > 5*time.Second || proof.at.After(time.Now()) {
					return ErrTextOwner
				}
				return guard(final)
			})
		})
	if err != nil {
		return nativewire.OutboundMedia{}, err
	}
	result, ok := value.(nativewire.OutboundMedia)
	if !ok || result.Validate() != nil {
		return nativewire.OutboundMedia{}, ErrTextRoute
	}
	return result, nil
}
