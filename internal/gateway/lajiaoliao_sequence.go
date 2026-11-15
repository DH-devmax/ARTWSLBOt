package gateway

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/lajiaoliao"
)

// Keep the first delivery immutable. A later recovery copy can add a sequence
// and server receipt, but must not become another chat event or ownership proof.
func (m *ReceiverManager) storeChiliSequence(ctx context.Context, tx pgx.Tx, account, key string,
	message *sdkws.MsgData, now time.Time) error {
	if message.Seq < 1 || !strings.HasPrefix(key, "lajiao:message:") {
		return nil
	}
	var sealed []byte
	err := tx.QueryRow(ctx, `SELECT payload_sealed FROM native_received_events
		WHERE account_id=$1 AND event_key=$2`, account, key).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		// A legacy sequence event already satisfied this recovery range.
		return nil
	}
	if err != nil {
		return err
	}
	if len(sealed) > 128<<10+64 {
		return &lajiaoliao.Failure{Code: "lajiaoliao_protocol_boundary"}
	}
	raw, err := m.Vault.Open("native-event:"+account+":"+key, sealed)
	if err != nil {
		return err
	}
	var first sdkws.MsgData
	err = proto.Unmarshal(raw, &first)
	clear(raw)
	defer clear(first.Content)
	if err != nil {
		return &lajiaoliao.Failure{Code: "lajiaoliao_protocol_boundary"}
	}
	if first.Seq > 0 {
		return nil
	}
	if first.ClientMsgID != message.ClientMsgID || first.SendID != message.SendID ||
		first.RecvID != message.RecvID || first.GroupID != message.GroupID ||
		first.SessionType != message.SessionType || first.MsgFrom != message.MsgFrom ||
		first.ContentType != message.ContentType || first.SendTime != message.SendTime ||
		first.ServerMsgID != "" && first.ServerMsgID != message.ServerMsgID ||
		!bytes.Equal(first.Content, message.Content) {
		return &lajiaoliao.Failure{Code: "lajiaoliao_protocol_boundary"}
	}
	evidenceKey := strings.Replace(key, "lajiao:message:", "lajiao:sequence:", 1)
	raw, err = proto.Marshal(message)
	if err != nil {
		return err
	}
	sealed, err = m.Vault.Seal("native-event:"+account+":"+evidenceKey, raw)
	clear(raw)
	if err != nil {
		return err
	}
	// The decoder only consumes private/group inputs. This notice supplements
	// the original event through indexed reads, without a second projection.
	_, err = tx.Exec(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
		VALUES($1,$2,'notice',$3,$4) ON CONFLICT(account_id,event_key) DO NOTHING`,
		account, evidenceKey, now, sealed)
	return err
}
