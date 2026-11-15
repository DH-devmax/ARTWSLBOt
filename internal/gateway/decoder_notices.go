package gateway

import (
	"context"
	"encoding/json"
	"strconv"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

func (m *DecoderManager) decodeTransportNotice(ctx context.Context, account string, outer nativewire.IMMessage) (DecodedMessage, string, string, bool) {
	if outer.Scene != 1 || outer.Target == "" || outer.Sender == "" ||
		outer.Fields[0] != "1" || outer.Fields[1] != outer.Target ||
		outer.Fields[2] != outer.Sender || outer.Fields[12] != outer.ServerID {
		return DecodedMessage{}, "", "", false
	}
	var groupID uint64
	notice := &nativewire.MessageNotice{Kind: "native_group_notification", Verification: "transport"}
	if outer.Fields[8] == "100" {
		var err error
		groupID, notice, err = nativewire.ReadUnencryptedGroupNotice(outer.Fields[10])
		if err != nil {
			return DecodedMessage{}, "", "", false
		}
	} else if outer.Fields[8] != "5" {
		return DecodedMessage{}, "", "", false
	}
	var sealed []byte
	if err := m.DB.QueryRow(ctx, `SELECT payload_sealed FROM native_directory_snapshots
		WHERE account_id=$1 AND octet_length(payload_sealed)<=1048576`, account).Scan(&sealed); err != nil {
		return DecodedMessage{}, "", "", false
	}
	plain, err := m.Vault.Open("native-directory:"+account, sealed)
	if err != nil {
		return DecodedMessage{}, "", "", false
	}
	var groups []nativeauth.DirectoryGroup
	err = json.Unmarshal(plain, &groups)
	clear(plain)
	if err != nil || len(groups) > 10000 {
		return DecodedMessage{}, "", "", false
	}
	return bindTransportNotice(outer, groupID, notice, groups)
}

// Directory matching chooses a display conversation only. Neither the legacy
// notification body nor the saved directory is evidence for native execution.
func bindTransportNotice(outer nativewire.IMMessage, claimedGroup uint64, notice *nativewire.MessageNotice,
	groups []nativeauth.DirectoryGroup) (DecodedMessage, string, string, bool) {
	var matched *nativeauth.DirectoryGroup
	var groupID uint64
	for index := range groups {
		group := &groups[index]
		id, err := strconv.ParseUint(group.ID, 10, 30)
		if group.Team != outer.Target || err != nil || id == 0 || strconv.FormatUint(id, 10) != group.ID ||
			claimedGroup != 0 && claimedGroup != id {
			continue
		}
		if matched != nil {
			return DecodedMessage{}, "", "", false
		}
		matched, groupID = group, id
	}
	if matched == nil || notice == nil || notice.Verification != "transport" {
		return DecodedMessage{}, "", "", false
	}
	out := DecodedMessage{TransportPeer: outer.Sender, TransportTo: outer.Target, ServerID: outer.ServerID,
		NativeFormat: outer.Fields[8], Mapping: "transport_notice"}
	out.Message = nativewire.ApplicationMessage{Session: 2, Format: 7, Notice: notice,
		Sender: nativewire.MessageSource{Name: "系统通知"},
		Target: nativewire.MessageSource{ID: groupID, Name: matched.Name}}
	return out, "group", conversationKey("group", groupID, outer.Target), true
}
