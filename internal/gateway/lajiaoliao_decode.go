package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/lajiaoliao"
)

func decodeChiliMessage(raw []byte, self string) (DecodedMessage, string, string, string, string) {
	var m sdkws.MsgData
	out := DecodedMessage{Platform: "lajiaoliao", Mapping: "native_identity"}
	if self == "" || len(self) > 128 || len(raw) > 128<<10 || proto.Unmarshal(raw, &m) != nil || len(m.Content) > 65536 {
		return out, "", "", "failed", "lajiaoliao_message_shape"
	}
	if m.ClientMsgID == "" {
		if m.Seq > 0 {
			return out, "", "", "unsupported", "lajiaoliao_sequence_placeholder"
		}
		return out, "", "", "failed", "lajiaoliao_message_shape"
	}
	if len(m.ClientMsgID) > 128 || m.SendID == "" || len(m.SendID) > 128 || len(m.RecvID) > 128 || len(m.GroupID) > 128 {
		return out, "", "", "failed", "lajiaoliao_message_shape"
	}
	out.NativeSender, out.NativeName, out.ServerID = m.SendID, m.SenderNickname, m.ServerMsgID
	out.TransportPeer, out.TransportTo = m.SendID, m.RecvID
	out.Message.ClientID = m.ClientMsgID
	out.Message.CreatedAt = uint64(max(m.SendTime, 0))
	out.Message.Sender.Name = m.SenderNickname
	out.NativeFormat = strconv.Itoa(int(m.ContentType))
	scene, target := "private", m.SendID
	if m.SessionType == 2 || m.SessionType == 3 {
		scene, target = "group", m.GroupID
		out.Message.Session = 2
		if m.GroupID == "" {
			return out, "", "", "failed", "lajiaoliao_message_routing"
		}
	} else if m.SessionType == 1 {
		out.Message.Session = 1
		if m.SendID == self {
			target = m.RecvID
			out.Outgoing = true
		} else if m.RecvID != self {
			return out, "", "", "failed", "lajiaoliao_message_routing"
		}
		if target == "" {
			return out, "", "", "failed", "lajiaoliao_message_routing"
		}
	} else {
		return out, "", "", "unsupported", "lajiaoliao_session_type"
	}
	out.NativeTarget = target
	if scene == "group" {
		out.Outgoing = m.SendID == self
	}
	key := conversationKey("lajiaoliao:"+scene, 0, target)
	// An unsupported component remains explicitly unsupported; no quote,
	// mention or media component is discarded to fabricate a text success.
	if m.ContentType != 101 || m.MsgFrom != 100 {
		return out, scene, key, "unsupported", "lajiaoliao_content_type"
	}
	var text struct {
		Content *string `json:"content"`
	}
	decoder := json.NewDecoder(bytes.NewReader(m.Content))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&text) != nil || text.Content == nil || len(*text.Content) > 16384 || !utf8.ValidString(*text.Content) {
		return out, scene, key, "failed", "lajiaoliao_text_shape"
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return out, scene, key, "failed", "lajiaoliao_text_shape"
	}
	out.Message.Text = *text.Content
	return out, scene, key, "decoded", ""
}

func (m *DecoderManager) decodeChili(ctx context.Context, job decodeJob) error {
	start := time.Now()
	var raw, session []byte
	var epoch int64
	if err := m.DB.QueryRow(ctx, `SELECT e.payload_sealed,a.credential_sealed,a.epoch FROM native_received_events e
        JOIN accounts a ON a.id=e.account_id WHERE e.id=$1 AND e.account_id=$2 AND a.platform='lajiaoliao'
        AND a.auth_state='authenticated' AND e.received_at>now()-interval '7 days'`, job.ID, job.Account).Scan(&raw, &session, &epoch); err != nil {
		return err
	}
	plain, err := m.Vault.Open("lajiaoliao-session:"+job.Account, session)
	if err != nil {
		return err
	}
	var s lajiaoliao.Session
	err = json.Unmarshal(plain, &s)
	clear(plain)
	if err != nil {
		return err
	}
	source, err := m.Vault.Open("native-event:"+job.Account+":"+job.Key, raw)
	if err != nil {
		return err
	}
	decoded, scene, key, state, reason := decodeChiliMessage(source, s.UserID)
	clear(source)
	var sealed []byte
	if key != "" {
		plain, err = json.Marshal(decoded)
		if err != nil {
			return err
		}
		sealed, err = m.Vault.Seal(DecodedAAD(job.Account, job.ID), plain)
		clear(plain)
		if err != nil {
			return err
		}
	}
	tag, err := m.DB.Exec(ctx, `UPDATE native_decoded_events x SET state=$1,scene=$2,conversation_key=$3,reason=$4,
        payload_sealed=$5,decoded_at=now(),decode_ms=$6,owner='',lease_until=NULL WHERE event_id=$7 AND account_id=$8
        AND state='processing' AND owner=$9 AND lease_until>now() AND received_at>now()-interval '7 days'
        AND EXISTS(SELECT 1 FROM accounts a WHERE a.id=x.account_id AND a.epoch=$10 AND a.platform='lajiaoliao'
            AND a.auth_state='authenticated')`, state, scene, key, reason, sealed, float64(time.Since(start).Microseconds())/1000,
		job.ID, job.Account, m.owner, epoch)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("decoder_scope_changed")
	}
	m.Changes.Publish(Change{Account: job.Account, Kind: "decoded", EventID: job.ID, Conversation: key})
	return nil
}
