package gateway

import (
	"context"
	"encoding/json"
	"strconv"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

type privateFriendReader interface {
	Friends(context.Context) ([]nativeauth.Friend, error)
}

func verifyPrivateRoster(roster nativeauth.MemberDirectory, self uint64, peer nativewire.PrivateTarget) error {
	if peer.ID == self || peer.ID == 0 || peer.ID >= 1<<30 || !nativewire.ValidPrivateTransport(peer.Transport) ||
		!roster.Complete || !roster.MappingComplete {
		return ErrTextRoute
	}
	found := false
	for _, member := range roster.Members {
		if member.ID != strconv.FormatUint(peer.ID, 10) && member.Transport != peer.Transport {
			continue
		}
		if found || member.ID != strconv.FormatUint(peer.ID, 10) || member.Transport != peer.Transport {
			return ErrTextRoute
		}
		found = true
	}
	if !found {
		return ErrTextRoute
	}
	return nil
}

// A private test target must still belong to the granted group, have the exact
// UID/NIM mapping, and be a current friend. A conversation alone grants nothing.
func (m *ReceiverManager) provePrivateTarget(ctx context.Context, client textReadClient, i TextIntent,
	connection *ownedTextConnection, roster nativeauth.MemberDirectory, key []byte) error {
	if i.PrivatePeer == nil || i.PrivateEventID < 1 || len(i.PrivateConversation) != 64 ||
		verifyPrivateRoster(roster, connection.uid, *i.PrivatePeer) != nil {
		return ErrTextRoute
	}
	reader, ok := client.(privateFriendReader)
	if !ok {
		return ErrTextRoute
	}
	friends, err := reader.Friends(ctx)
	if err != nil {
		return err
	}
	count := 0
	for _, friend := range friends {
		if friend.ID == strconv.FormatUint(i.PrivatePeer.ID, 10) {
			count++
		}
	}
	if count != 1 {
		return ErrTextRoute
	}
	var sealed []byte
	var eventKey string
	err = m.DB.QueryRow(ctx, `SELECT e.payload_sealed,e.event_key FROM native_received_events e
        JOIN native_decoded_events d ON d.event_id=e.id AND d.account_id=e.account_id
        WHERE e.id=$1 AND e.account_id=$2 AND e.kind='private' AND d.scene='private' AND d.state='decoded'
        AND d.conversation_key=$3 AND d.deployment_version=$4
        AND e.payload_sealed IS NOT NULL AND d.payload_sealed IS NOT NULL
        AND e.received_at>clock_timestamp()-interval '7 days'+interval '3 seconds'
        AND d.received_at>clock_timestamp()-interval '7 days'+interval '3 seconds'
        AND e.received_at<=clock_timestamp() AND d.received_at<=clock_timestamp()`,
		i.PrivateEventID, i.AccountID, i.PrivateConversation, connection.version).Scan(&sealed, &eventKey)
	if err != nil {
		return ErrTextRoute
	}
	plain, err := m.Vault.Open("native-event:"+i.AccountID+":"+eventKey, sealed)
	if err != nil {
		return ErrTextRoute
	}
	var outer nativewire.IMMessage
	err = json.Unmarshal(plain, &outer)
	clear(plain)
	if err != nil || outer.Scene != 0 || outer.Fields[0] != "0" || outer.Fields[1] != outer.Target ||
		outer.Fields[2] != outer.Sender || outer.Fields[8] != "100" || outer.Fields[12] != outer.ServerID {
		return ErrTextRoute
	}
	inner, err := nativewire.OpenMessage(key, outer.Fields[10])
	if err != nil || inner.ClientID != outer.Fields[11] {
		return ErrTextRoute
	}
	peer, err := nativewire.PrivateTargetFor(connection.uid, connection.sender, inner, outer.Sender, outer.Target)
	if err != nil || peer != *i.PrivatePeer || nativewire.PrivateConversationKey(peer) != i.PrivateConversation {
		return ErrTextRoute
	}
	return nil
}
