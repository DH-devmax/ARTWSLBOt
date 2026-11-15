package gateway

import (
	"context"
	"fmt"
	"time"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

type ChiliMediaIntent struct {
	AccountID, ReceiverOwner, Sender, Group, Peer, Operation, ClientID string
	AccountEpoch, Revision                                             int64
	Asset                                                              lajiaoliao.MediaAsset
	Input                                                              nativeauth.MessageUpload
	Message                                                            lajiaoliao.Outbound
}

type chiliMediaTransport interface {
	SendMedia(context.Context, string, string, lajiaoliao.MediaAsset,
		func(context.Context, func(context.Context) error) error) (lajiaoliao.Receipt, error)
}

type chiliOutboundTransport interface {
	SendOutbound(context.Context, string, string, string, lajiaoliao.Outbound,
		func(context.Context, func(context.Context) error) error) (lajiaoliao.Receipt, error)
}

func (m *ReceiverManager) chiliMediaOwner(i ChiliMediaIntent) (*receiverWorker, *ownedChiliConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.jobs[i.AccountID]
	if w == nil || i.ReceiverOwner != m.owner || w.spec.Epoch != i.AccountEpoch ||
		w.spec.Revision != i.Revision || w.spec.Platform != "lajiaoliao" || w.spec.UID != i.Sender {
		return nil, nil, ErrTextOwner
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	c, now := w.chiliConnection, time.Now()
	if c == nil || c.ctx.Err() != nil || c.uid != i.Sender || w.snapshot.State != "connected" ||
		w.snapshot.HeartbeatAt == nil || w.snapshot.HeartbeatAt.After(now) ||
		now.Sub(*w.snapshot.HeartbeatAt) > 37*time.Second || !w.leaseUntil.After(now.Add(2*time.Second)) {
		return nil, nil, ErrTextOwner
	}
	if _, ok := c.transport.(chiliMediaTransport); !ok {
		return nil, nil, ErrTextOwner
	}
	select {
	case <-c.transport.Done():
		return nil, nil, ErrTextOwner
	default:
	}
	return w, c, nil
}

func proveChiliMedia(ctx context.Context, client *lajiaoliao.Client, session lajiaoliao.Session, group string) (time.Time, error) {
	info, err := client.GroupInfo(ctx, session, group)
	if err != nil {
		return time.Time{}, err
	}
	self, err := client.Member(ctx, session, group, session.UserID)
	if err != nil {
		return time.Time{}, err
	}
	if info.Status == nil || *info.Status != 0 || self.Role != 60 && self.Role != 100 ||
		self.MuteEnd > time.Now().Unix() {
		return time.Time{}, ErrTextRoute
	}
	return time.Now(), nil
}

func (m *ReceiverManager) UploadChiliMedia(ctx context.Context, i ChiliMediaIntent,
	guard func(context.Context) error) (lajiaoliao.MediaAsset, error) {
	var empty lajiaoliao.MediaAsset
	if guard == nil || i.Operation == "" || !lajiaoliao.ValidID(i.Group) ||
		i.Input.Kind != "image" && i.Input.Kind != "voice" && i.Input.Kind != "video" ||
		i.Peer != "" && (!lajiaoliao.ValidID(i.Peer) || i.Input.Kind != "image") {
		return empty, ErrTextRoute
	}
	meta, err := nativeauth.InspectUpload(i.Input)
	if err != nil {
		return empty, err
	}
	if i.Input.Kind == "video" {
		width, height, err := lajiaoliao.SnapshotDimensions(i.Input.Thumbnail)
		if err != nil {
			return empty, err
		}
		meta.Width, meta.Height = uint64(width), uint64(height)
	}
	w, connection, err := m.chiliMediaOwner(i)
	if err != nil {
		return empty, err
	}
	ctx, cancel := context.WithCancel(ctx)
	follow := context.AfterFunc(connection.ctx, cancel)
	defer func() { follow(); cancel() }()
	value, err := m.readValue(ctx, i.AccountID, "chili-media-upload:"+i.Operation, ReadRefresh, func(work context.Context) (any, error) {
		session, err := m.loadChili(work, w)
		if err != nil || session.UserID != i.Sender {
			return nil, ErrTextOwner
		}
		client := lajiaoliao.NewClient()
		defer client.Close()
		at, err := proveChiliMedia(work, client, session, i.Group)
		if err != nil {
			return nil, err
		}
		if i.Peer != "" {
			if _, err = client.Member(work, session, i.Group, i.Peer); err != nil {
				return nil, err
			}
		}
		return client.UploadMedia(work, session, i.Operation, lajiaoliao.MediaUpload{
			Kind: i.Input.Kind, Name: i.Input.Name, Data: i.Input.Data, Thumbnail: i.Input.Thumbnail,
			Duration: int64(meta.Duration), Width: int64(meta.Width), Height: int64(meta.Height),
		}, func(final context.Context) error {
			_, current, err := m.chiliMediaOwner(i)
			if err != nil || current != connection || time.Since(at) > 5*time.Second {
				return ErrTextOwner
			}
			return guard(final)
		})
	})
	if err != nil {
		return empty, err
	}
	asset, ok := value.(lajiaoliao.MediaAsset)
	if !ok || asset.Validate() != nil {
		return empty, ErrTextRoute
	}
	return asset, nil
}

func (m *ReceiverManager) SubmitChiliMedia(ctx context.Context, i ChiliMediaIntent,
	fence nativewire.SubmissionFence) (lajiaoliao.Receipt, error) {
	message := i.Message
	if message.Media == nil && i.Asset.Kind != "" {
		message.Media = &i.Asset
	}
	content, _, shapeErr := message.Content()
	clear(content)
	if fence == nil || shapeErr != nil || !lajiaoliao.ValidID(i.Group) || !lajiaoliao.ValidID(i.ClientID) ||
		i.Peer != "" && (!lajiaoliao.ValidID(i.Peer) || message.Mention != nil || message.Quote != nil ||
			message.Media != nil && message.Media.Kind != "image") ||
		message.Quote != nil && message.Quote.Group != i.Group {
		return lajiaoliao.Receipt{}, ErrTextRoute
	}
	w, connection, err := m.chiliMediaOwner(i)
	if err != nil {
		return lajiaoliao.Receipt{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	follow := context.AfterFunc(connection.ctx, cancel)
	defer func() { follow(); cancel() }()
	value, err := m.readValue(ctx, i.AccountID, fmt.Sprintf("chili-media-proof:%s", i.ClientID), ReadRefresh,
		func(work context.Context) (any, error) {
			session, err := m.loadChili(work, w)
			if err != nil || session.UserID != i.Sender {
				return nil, ErrTextOwner
			}
			client := lajiaoliao.NewClient()
			defer client.Close()
			at, err := proveChiliMedia(work, client, session, i.Group)
			if err != nil {
				return nil, err
			}
			target := i.Peer
			if message.Mention != nil {
				target = message.Mention.ID
			}
			if target != "" {
				if _, err = client.Member(work, session, i.Group, target); err != nil {
					return nil, err
				}
			}
			return at, nil
		})
	if err != nil {
		return lajiaoliao.Receipt{}, err
	}
	at, ok := value.(time.Time)
	if !ok {
		return lajiaoliao.Receipt{}, ErrTextRoute
	}
	guard := func(work context.Context, write func(context.Context) error) error {
		return fence(work, func(final context.Context) error {
			_, current, err := m.chiliMediaOwner(i)
			if err != nil || current != connection || time.Since(at) > 5*time.Second || at.After(time.Now()) {
				return ErrTextOwner
			}
			return write(final)
		})
	}
	if transport, ok := connection.transport.(chiliOutboundTransport); ok {
		return transport.SendOutbound(ctx, i.Group, i.Peer, i.ClientID, message, guard)
	}
	if message.Media != nil && i.Peer == "" {
		return connection.transport.(chiliMediaTransport).SendMedia(ctx, i.Group, i.ClientID, *message.Media, guard)
	}
	return lajiaoliao.Receipt{}, ErrTextOwner
}
