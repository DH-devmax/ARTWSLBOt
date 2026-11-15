package gateway

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/lajiaoliao"
)

type chiliMediaOwnerTransport struct{ chiliObservationTransport }

func (c *chiliMediaOwnerTransport) SendMedia(context.Context, string, string, lajiaoliao.MediaAsset,
	func(context.Context, func(context.Context) error) error) (lajiaoliao.Receipt, error) {
	return lajiaoliao.Receipt{}, nil
}

func TestChiliMediaOwnerUsesOneCurrentConnection(t *testing.T) {
	for _, mode := range []string{"valid", "owner", "epoch", "revision", "platform", "identity", "heartbeat", "lease", "state", "cancelled", "closed"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			now := time.Now()
			transport := &chiliMediaOwnerTransport{chiliObservationTransport{done: make(chan struct{}), hints: make(chan struct{}, 1)}}
			connection := &ownedChiliConnection{transport: transport, ctx: ctx, uid: "string-self"}
			worker := &receiverWorker{spec: receiverSpec{ID: "account", UID: "string-self", Platform: "lajiaoliao", Epoch: 1, Revision: 2},
				chiliConnection: connection, leaseUntil: now.Add(time.Minute), snapshot: ReceiverSnapshot{State: "connected", HeartbeatAt: &now}}
			manager := &ReceiverManager{owner: "owner", jobs: map[string]*receiverWorker{"account": worker}}
			intent := ChiliMediaIntent{AccountID: "account", ReceiverOwner: "owner", AccountEpoch: 1, Revision: 2, Sender: "string-self"}
			switch mode {
			case "owner":
				intent.ReceiverOwner = "replacement"
			case "epoch":
				intent.AccountEpoch++
			case "revision":
				intent.Revision++
			case "platform":
				worker.spec.Platform = "wsl"
			case "identity":
				intent.Sender = "other"
			case "heartbeat":
				old := now.Add(-time.Minute)
				worker.snapshot.HeartbeatAt = &old
			case "lease":
				worker.leaseUntil = now
			case "state":
				worker.snapshot.State = "synchronizing"
			case "cancelled":
				cancel()
			case "closed":
				transport.Close()
			}
			_, current, err := manager.chiliMediaOwner(intent)
			if mode == "valid" {
				if err != nil || current != connection {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("replaced or stale media owner accepted")
			}
		})
	}
}

func TestChiliMediaEchoMatchesNativeStringsAndExactContent(t *testing.T) {
	content := []byte(`{"duration":1,"sourceUrl":"https://lj-im-chat-01.lajiaoliao.com/synthetic"}`)
	digest := sha256.Sum256(content)
	query := lajiaoliao.MediaEvidenceQuery{Sender: "string-self", Group: "string-group", ClientID: "string-client", Digest: digest[:]}
	base := &sdkws.MsgData{SendID: query.Sender, RecvID: query.Group, GroupID: query.Group, ClientMsgID: query.ClientID,
		ServerMsgID: "string-server", SessionType: 3, MsgFrom: 100, ContentType: 103, Content: content, SendTime: 1700000000123}
	for _, mode := range []string{"valid", "video", "sender", "group", "recipient", "client", "server", "time", "private", "type", "content"} {
		t.Run(mode, func(t *testing.T) {
			m := proto.Clone(base).(*sdkws.MsgData)
			switch mode {
			case "video":
				m.ContentType = 104
			case "sender":
				m.SendID = "other"
			case "group":
				m.GroupID = "other"
			case "recipient":
				m.RecvID = "other"
			case "client":
				m.ClientMsgID = "other"
			case "server":
				m.ServerMsgID = ""
			case "time":
				m.SendTime = 0
			case "private":
				m.SessionType = 1
			case "type":
				m.ContentType = 101
			case "content":
				m.Content = append(m.Content, ' ')
			}
			raw, _ := proto.Marshal(m)
			receipt, matched := matchChiliMediaEcho(query, raw)
			clear(raw)
			if matched != (mode == "valid" || mode == "video") {
				t.Fatal("conflicting echo accepted", mode)
			}
			if matched && receipt.ServerID != "string-server" {
				t.Fatal("string native ID lost")
			}
		})
	}
}

func TestChiliOutboundEchoBindsTypeAndPrivatePeer(t *testing.T) {
	content := []byte(`{"text":"synthetic"}`)
	digest := sha256.Sum256(content)
	for _, kind := range []int32{101, 102, 103, 104, 106, 114} {
		query := lajiaoliao.MediaEvidenceQuery{Sender: "self", Group: "group", Peer: "peer",
			ClientID: "client", Digest: digest[:], ContentType: kind}
		m := &sdkws.MsgData{SendID: "self", RecvID: "peer", ClientMsgID: "client", ServerMsgID: "server",
			SessionType: 1, MsgFrom: 100, ContentType: kind, Content: content, SendTime: 1700000000123}
		raw, _ := proto.Marshal(m)
		if _, matched := matchChiliMediaEcho(query, raw); !matched {
			t.Fatalf("matching private type %d was rejected", kind)
		}
		query.ContentType = kind + 1
		if _, matched := matchChiliMediaEcho(query, raw); matched {
			t.Fatalf("mismatched private type %d was accepted", kind)
		}
		query.ContentType, query.Peer = kind, "another-peer"
		if _, matched := matchChiliMediaEcho(query, raw); matched {
			t.Fatal("another private peer was accepted")
		}
		clear(raw)
	}
}
