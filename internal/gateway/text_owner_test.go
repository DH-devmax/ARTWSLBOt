package gateway

import (
	"context"
	"crypto/sha256"
	"slices"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

func TestVerifiedTextRoster(t *testing.T) {
	valid := nativeauth.MemberDirectory{Group: "123", Complete: true,
		Members: []nativeauth.GroupMember{{ID: "7", Transport: "me", Role: "admin"}}}
	if err := verifyTextRoster(valid, "123", 7, "me"); err != nil {
		t.Fatal(err)
	}
	valid.Members[0].Role = "member"
	if err := verifyTextRoster(valid, "123", 7, "me"); err != nil {
		t.Fatal("verified ordinary membership was confused with moderation permission", err)
	}
	for _, change := range []func(*nativeauth.MemberDirectory){
		func(d *nativeauth.MemberDirectory) { d.Group = "other" },
		func(d *nativeauth.MemberDirectory) { d.Complete = false },
		func(d *nativeauth.MemberDirectory) { d.Members = nil },
		func(d *nativeauth.MemberDirectory) { d.Members[0].Transport = "" },
		func(d *nativeauth.MemberDirectory) { d.Members[0].Transport = "someone-else" },
		func(d *nativeauth.MemberDirectory) { d.Members[0].Role = "unknown" },
		func(d *nativeauth.MemberDirectory) { d.Members = append(d.Members, d.Members[0]) },
	} {
		d := valid
		d.Members = slices.Clone(valid.Members)
		change(&d)
		if err := verifyTextRoster(d, "123", 7, "me"); err == nil {
			t.Fatal("unverified current identity granted text route")
		}
	}
}

func TestTextMuteAndMentionEvidence(t *testing.T) {
	roster := nativeauth.MemberDirectory{Group: "123", Complete: true,
		Members: []nativeauth.GroupMember{
			{ID: "7", Transport: "me", Role: "admin", Name: "self"},
			{ID: "8", Transport: "8", Role: "member", Name: "name", Card: "current-card"},
		}}
	if err := verifyTextMuteRoster(roster, map[string]bool{"8": true}, "me"); err != nil {
		t.Fatal("another member's mute blocked self", err)
	}
	for _, muted := range []map[string]bool{nil, {"me": true}, {"not-in-roster": true}} {
		if err := verifyTextMuteRoster(roster, muted, "me"); err == nil {
			t.Fatal("missing/mismatched/self-muted evidence granted speech")
		}
	}
	broken := roster
	broken.Members = append(slices.Clone(roster.Members), roster.Members[0])
	if err := verifyTextMuteRoster(broken, map[string]bool{}, "me"); err == nil {
		t.Fatal("ambiguous member mapping granted speech")
	}
	for _, mention := range []nativewire.MessageMention{
		{UID: 8, Name: "current-card"}, {UID: 8, Name: "old-card"}, {UID: 9, Name: "current-card"},
	} {
		err := verifyTextMentions(roster, []nativewire.MessageMention{mention})
		if (err == nil) != (mention.UID == 8 && mention.Name == "current-card") {
			t.Fatal("mention trusted stale or foreign member evidence")
		}
	}
}

func TestTextOwnerRejectsStaleOrReplacedConnections(t *testing.T) {
	for _, mode := range []string{"valid", "wrong_owner", "epoch", "revision", "heartbeat", "lease", "syncing", "closed", "cancelled", "identity"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			now := time.Now()
			c := &ownedTextConnection{ctx: ctx, uid: 7, sender: "me", transport: &IMTransport{done: make(chan struct{})}}
			w := &receiverWorker{spec: receiverSpec{ID: "a", Epoch: 1, Revision: 2}, leaseUntil: now.Add(time.Minute),
				connection: c, snapshot: ReceiverSnapshot{State: "connected", HeartbeatAt: &now}}
			m := &ReceiverManager{owner: "owner", jobs: map[string]*receiverWorker{"a": w}}
			i := TextIntent{AccountID: "a", ReceiverOwner: "owner", AccountEpoch: 1, Revision: 2,
				Message: nativewire.GroupText{SenderID: 7}}
			switch mode {
			case "wrong_owner":
				i.ReceiverOwner = "replacement"
			case "epoch":
				i.AccountEpoch++
			case "revision":
				i.Revision++
			case "heartbeat":
				old := now.Add(-time.Minute)
				w.snapshot.HeartbeatAt = &old
			case "lease":
				w.leaseUntil = now
			case "syncing":
				w.snapshot.State = "synchronizing"
			case "closed":
				close(c.transport.done)
			case "cancelled":
				cancel()
			case "identity":
				i.Message.SenderID++
			}
			_, got, err := m.textOwner(i)
			if mode == "valid" {
				if err != nil || got != c {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid connection owner accepted")
			}
		})
	}
}

func TestTextEchoRequiresExactCiphertextAndRoute(t *testing.T) {
	hash := sha256.Sum256([]byte("synthetic-ciphertext"))
	q := nativewire.TextEvidenceQuery{ClientID: outboundClientID,
		Route: nativewire.TextRoute{Team: "789", Sender: "me", Digest: hash[:]}}
	valid := nativewire.IMMessage{Scene: 1, Target: "789", Sender: "me", ServerID: "9001",
		Fields: map[uint32]string{0: "1", 1: "789", 2: "me", 7: "1700000000123",
			8: "100", 10: "synthetic-ciphertext", 11: outboundClientID, 12: "9001"}}
	if _, matched := matchTextEcho(q, valid); !matched {
		t.Fatal("matching synthetic evidence rejected")
	}
	for _, field := range []uint32{0, 1, 2, 7, 8, 10, 11, 12} {
		changed := valid
		changed.Fields = make(map[uint32]string)
		for key, value := range valid.Fields {
			changed.Fields[key] = value
		}
		changed.Fields[field] = "changed"
		if _, matched := matchTextEcho(q, changed); matched {
			t.Fatalf("conflicting evidence field %d accepted", field)
		}
	}
	valid.Scene = 0
	if _, matched := matchTextEcho(q, valid); matched {
		t.Fatal("private echo accepted as group submission evidence")
	}
}

func TestPrivateEchoCannotMatchGroupOrOtherPeer(t *testing.T) {
	hash := sha256.Sum256([]byte("synthetic-private-ciphertext"))
	q := nativewire.TextEvidenceQuery{ClientID: outboundClientID,
		Route: nativewire.TextRoute{Team: "789", Sender: "me", PrivatePeer: "peer-8", Digest: hash[:]}}
	message := nativewire.IMMessage{Scene: 0, Target: "peer-8", Sender: "me", ServerID: "9001",
		Fields: map[uint32]string{0: "0", 1: "peer-8", 2: "me", 7: "1700000000123", 8: "100",
			10: "synthetic-private-ciphertext", 11: outboundClientID, 12: "9001"}}
	if _, ok := matchTextEcho(q, message); !ok {
		t.Fatal("exact private echo rejected")
	}
	message.Scene = 1
	if _, ok := matchTextEcho(q, message); ok {
		t.Fatal("group echo reconciled private message")
	}
	message.Scene, message.Target, message.Fields[1] = 0, "other-peer", "other-peer"
	if _, ok := matchTextEcho(q, message); ok {
		t.Fatal("private echo crossed peer")
	}
}
