package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

var ErrTextOwner = errors.New("native_text_connection_changed")
var ErrTextRoute = errors.New("native_text_route_unverified")

type ownedTextConnection struct {
	transport *IMTransport
	ctx       context.Context
	uid       uint64
	sender    string
	version   int64
}

// TextIntent is server-created. It is never decoded from a browser request.
type TextIntent struct {
	AccountID, ReceiverOwner string
	AccountEpoch, Revision   int64
	Message                  nativewire.GroupText
	QuoteEventID             int64
	PrivatePeer              *nativewire.PrivateTarget
	PrivateConversation      string
	PrivateEventID           int64
	Nonce, AAD               uint64
	RecordRoute              func(context.Context, nativewire.TextRoute) error
}

type textReadClient interface {
	Directory(context.Context, uint64) ([]nativeauth.DirectoryGroup, error)
	Members(context.Context, string) (nativeauth.MemberDirectory, error)
	Close()
}

type textProof struct {
	route nativewire.TextRoute
	key   [32]byte
	reply *nativewire.ReplyRoute
}

func (m *ReceiverManager) textOwner(i TextIntent) (*receiverWorker, *ownedTextConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.jobs[i.AccountID]
	if w == nil || i.ReceiverOwner != m.owner || w.spec.Epoch != i.AccountEpoch ||
		w.spec.Revision != i.Revision {
		return nil, nil, ErrTextOwner
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	c, now := w.connection, time.Now()
	if c == nil || c.ctx.Err() != nil || c.uid != i.Message.SenderID ||
		w.snapshot.State != "connected" || w.snapshot.HeartbeatAt == nil ||
		w.snapshot.HeartbeatAt.After(now) || now.Sub(*w.snapshot.HeartbeatAt) > 37*time.Second ||
		!w.leaseUntil.After(now.Add(2*time.Second)) {
		return nil, nil, ErrTextOwner
	}
	select {
	case <-c.transport.done:
		return nil, nil, ErrTextOwner
	default:
	}
	return w, c, nil
}

// SubmitText never opens a socket or logs in. Fresh directory/member reads use
// the account HTTP lane before the control fence, outside the receive/ACK path.
// It is not registered with any production operation endpoint.
func (m *ReceiverManager) SubmitText(ctx context.Context, i TextIntent, fence nativewire.SubmissionFence) (nativewire.TextSendResult, error) {
	return m.submitText(ctx, i, fence, func(d nativeauth.Deployment, s nativeauth.Session, ids nativeauth.RequestIDSource) (textReadClient, error) {
		return nativeauth.Resume(d, s, ids)
	})
}

func (m *ReceiverManager) submitText(ctx context.Context, i TextIntent, fence nativewire.SubmissionFence,
	factory func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (textReadClient, error)) (nativewire.TextSendResult, error) {
	var empty nativewire.TextSendResult
	if fence == nil || i.RecordRoute == nil || i.Nonce == 0 || i.AAD == 0 ||
		(i.Message.Quote == nil) != (i.QuoteEventID == 0) || i.QuoteEventID < 0 ||
		i.PrivatePeer != nil && (i.Message.Quote != nil || len(i.Message.Mentions) != 0 ||
			i.Message.Media != nil && i.Message.Media.Kind != "image") ||
		i.PrivatePeer == nil && (i.PrivateEventID != 0 || i.PrivateConversation != "") {
		return empty, ErrTextRoute
	}
	w, connection, err := m.textOwner(i)
	if err != nil {
		return empty, err
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(connection.ctx, cancel)
	defer func() { stop(); cancel() }()
	value, err := m.readValue(ctx, i.AccountID, fmt.Sprintf("text-proof:%d:%d:%s", i.AccountEpoch, i.Revision, i.Message.ClientID),
		ReadRefresh, func(work context.Context) (any, error) {
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
			group := strconv.FormatUint(i.Message.GroupID, 10)
			current, err := proveNativeGroup(work, client, connection, group)
			if err != nil {
				return nil, err
			}
			if current.muted || verifyTextRoster(current.roster, group, connection.uid, connection.sender) != nil {
				return nil, ErrTextRoute
			}
			if verifyTextMentions(current.roster, i.Message.Mentions) != nil {
				return nil, ErrTextRoute
			}
			keyText := d.MessageKey
			if keyText == "" && d.ProtocolVersion == nativeauth.DesktopProtocolVersion {
				keyText = d.BodyKey
			}
			key, err := base64.StdEncoding.Strict().DecodeString(keyText)
			if err != nil || len(key) != 32 {
				clear(key)
				return nil, ErrTextRoute
			}
			proof := textProof{route: nativewire.TextRoute{Team: current.group.Team, Sender: connection.sender,
				DeploymentVersion: saved.DeploymentVersion, ObservedAt: current.at}}
			copy(proof.key[:], key)
			clear(key)
			if i.Message.Quote != nil {
				proof.reply, err = m.resolveTextReply(work, i, proof.key[:], current.group.Team, current.roster, connection.version)
				if err != nil {
					clear(proof.key[:])
					return nil, err
				}
			}
			if i.PrivatePeer != nil {
				if err = m.provePrivateTarget(work, client, i, connection, current.roster, proof.key[:]); err != nil {
					clear(proof.key[:])
					return nil, err
				}
				proof.route.PrivatePeer = i.PrivatePeer.Transport
			}
			return proof, nil
		})
	if err != nil {
		return empty, err
	}
	proof, ok := value.(textProof)
	if !ok {
		return empty, ErrTextRoute
	}
	defer clear(proof.key[:])
	var attachment string
	if i.PrivatePeer != nil {
		attachment, err = nativewire.SealPrivateText(proof.key[:], nativewire.PrivateText{
			SenderID: i.Message.SenderID, PeerID: i.PrivatePeer.ID, ClientID: i.Message.ClientID,
			Text: i.Message.Text, CreatedAt: i.Message.CreatedAt, Media: i.Message.Media,
		}, i.Nonce, i.AAD)
	} else {
		attachment, err = nativewire.SealGroupText(proof.key[:], i.Message, i.Nonce, i.AAD)
	}
	if err != nil {
		return empty, err
	}
	hash := sha256.Sum256([]byte(attachment))
	proof.route.Digest = hash[:]
	if _, current, err := m.textOwner(i); err != nil || current != connection {
		return empty, ErrTextOwner
	}
	if err = i.RecordRoute(ctx, proof.route); err != nil {
		return empty, err
	}
	finalFence := func(work context.Context, write func(context.Context) error) error {
		return fence(work, func(final context.Context) error {
			_, current, err := m.textOwner(i)
			if err != nil || current != connection || time.Since(proof.route.ObservedAt) > 5*time.Second ||
				proof.route.ObservedAt.After(time.Now()) {
				return ErrTextOwner
			}
			return write(final)
		})
	}
	if i.PrivatePeer != nil {
		return connection.transport.sendPrivateText(ctx, proof.route.PrivatePeer, i.Message.ClientID, attachment, finalFence)
	}
	return connection.transport.sendGroupTextReply(ctx, proof.route.Team, i.Message.ClientID, attachment, proof.reply, finalFence)
}

func verifyTextRoster(roster nativeauth.MemberDirectory, group string, uid uint64, sender string) error {
	if !roster.Complete || roster.Group != group || sender == "" {
		return ErrTextRoute
	}
	found := false
	for _, member := range roster.Members {
		if member.ID != strconv.FormatUint(uid, 10) {
			continue
		}
		if found || member.Transport != sender || member.Role != "member" && member.Role != "admin" && member.Role != "owner" {
			return ErrTextRoute
		}
		found = true
	}
	if !found {
		return ErrTextRoute
	}
	return nil
}

func verifyTextMuteRoster(roster nativeauth.MemberDirectory, muted map[string]bool, self string) error {
	if !roster.Complete || muted == nil {
		return ErrTextRoute
	}
	peers, ids := make(map[string]bool), make(map[string]bool)
	for _, member := range roster.Members {
		if member.ID == "" || member.Transport == "" || peers[member.Transport] || ids[member.ID] {
			return ErrTextRoute
		}
		peers[member.Transport], ids[member.ID] = true, true
	}
	if self != "" && (!peers[self] || muted[self]) {
		return ErrTextRoute
	}
	for peer := range muted {
		if !peers[peer] {
			return ErrTextRoute
		}
	}
	return nil
}

func verifyTextMentions(roster nativeauth.MemberDirectory, mentions []nativewire.MessageMention) error {
	for _, mention := range mentions {
		found := false
		for _, member := range roster.Members {
			if member.ID != strconv.FormatUint(mention.UID, 10) {
				continue
			}
			name := member.Card
			if name == "" {
				name = member.Name
			}
			if found || mention.Name != name || name == "" {
				return ErrTextRoute
			}
			found = true
		}
		if !found {
			return ErrTextRoute
		}
	}
	return nil
}
