package control

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"testing"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/lajiaoliao"
)

// The existing fixture guards serial execution against wsl_test and creates
// synthetic accounts only. Sequence notices supplement a source, not its ID.
func TestPostgresChiliSourceCanonicalSequenceEvidence(t *testing.T) {
	for _, mode := range []string{"matching-evidence", "wrong-account", "expired-evidence"} {
		t.Run(mode, func(t *testing.T) {
			f := newBusinessFixture(t)
			req := TextRequest{AccountID: f.account("lajiaoliao", 1)}
			if err := f.db.Platform.QueryRow(f.ctx, `SELECT epoch FROM accounts WHERE id=$1`, req.AccountID).
				Scan(&req.AccountEpoch); err != nil {
				t.Fatal(err)
			}
			service := &NativeTextService{DB: f.db, Vault: f.service.Vault}
			conversation := "synthetic:sequence-group"
			message := &sdkws.MsgData{ClientMsgID: "synthetic-sequence-client", SendID: "synthetic-sender",
				RecvID: "synthetic-group", GroupID: "synthetic-group", SessionType: 3,
				MsgFrom: 100, ContentType: 101, SendTime: time.Now().UnixMilli(),
				Content: []byte(`{"content":"synthetic sequence reference"}`)}
			canonicalRaw, err := proto.Marshal(message)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(canonicalRaw)
			keyHash := sha256.Sum256([]byte("sg_" + message.GroupID + "\x00" + message.ClientMsgID))
			suffix := hex.EncodeToString(keyHash[:])
			canonicalKey, evidenceKey := "lajiao:message:"+suffix, "lajiao:sequence:"+suffix
			sealed, err := service.Vault.Seal("native-event:"+req.AccountID+":"+canonicalKey, canonicalRaw)
			if err != nil {
				t.Fatal(err)
			}
			var canonicalID int64
			if err := f.db.Platform.QueryRow(f.ctx, `INSERT INTO native_received_events
				(account_id,event_key,kind,received_at,payload_sealed)
				VALUES($1,$2,'group',clock_timestamp(),$3) RETURNING id`, req.AccountID, canonicalKey, sealed).
				Scan(&canonicalID); err != nil {
				t.Fatal(err)
			}
			f.exec(`INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,
				deployment_version,codec_version,received_at,decoded_at)
				VALUES($1,$2,'decoded','group',$3,1,7,clock_timestamp(),clock_timestamp())`,
				canonicalID, req.AccountID, conversation)
			resolve := func(request TextRequest, eventID int64) (lajiaoliao.OutboundSource, []byte, error) {
				t.Helper()
				tx, err := f.db.Platform.Begin(f.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(f.ctx)
				return service.chiliSource(f.ctx, tx, request, eventID, "group", conversation)
			}
			want := lajiaoliao.OutboundSource{ClientID: message.ClientMsgID, Sender: message.SendID,
				Recipient: message.RecvID, Group: message.GroupID, Session: 3,
				Time: message.SendTime, Text: "synthetic sequence reference"}
			canonicalDigest := sha256.Sum256(canonicalRaw)
			source, digest, err := resolve(req, canonicalID)
			if err != nil || !reflect.DeepEqual(source, want) || !bytes.Equal(digest, canonicalDigest[:]) {
				t.Fatal("original Seq0 source did not remain readable without quote admission", source, err)
			}

			evidence := proto.Clone(message).(*sdkws.MsgData)
			evidence.Seq, evidence.ServerMsgID = 7, "synthetic-sequence-server"
			evidenceRaw, err := proto.Marshal(evidence)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(evidenceRaw)
			evidenceReq := req
			if mode == "wrong-account" {
				evidenceReq.AccountID = f.account("lajiaoliao", 2)
				if err := f.db.Platform.QueryRow(f.ctx, `SELECT epoch FROM accounts WHERE id=$1`, evidenceReq.AccountID).
					Scan(&evidenceReq.AccountEpoch); err != nil {
					t.Fatal(err)
				}
			}
			sealed, err = service.Vault.Seal("native-event:"+evidenceReq.AccountID+":"+evidenceKey, evidenceRaw)
			if err != nil {
				t.Fatal(err)
			}
			var evidenceID int64
			if err := f.db.Platform.QueryRow(f.ctx, `INSERT INTO native_received_events
				(account_id,event_key,kind,received_at,payload_sealed)
				VALUES($1,$2,'notice',clock_timestamp(),$3) RETURNING id`, evidenceReq.AccountID, evidenceKey, sealed).
				Scan(&evidenceID); err != nil {
				t.Fatal(err)
			}
			if evidenceID <= canonicalID {
				t.Fatal("fixture did not retain the primary event before sequence evidence")
			}
			if mode == "expired-evidence" {
				f.exec(`UPDATE native_received_events SET received_at=clock_timestamp()-interval '8 days' WHERE id=$1`,
					evidenceID)
			}
			expectedDigest := canonicalDigest[:]
			if mode == "matching-evidence" {
				want.Seq, want.ServerID, want.CanQuote = evidence.Seq, evidence.ServerMsgID, true
				evidenceDigest := sha256.Sum256(evidenceRaw)
				expectedDigest = evidenceDigest[:]
			}
			source, digest, err = resolve(req, canonicalID)
			if err != nil || !reflect.DeepEqual(source, want) || !bytes.Equal(digest, expectedDigest) {
				t.Fatal("canonical event selected the wrong evidence, fingerprint or quote admission", source, err)
			}
			if _, digest, err := resolve(evidenceReq, evidenceID); err == nil || len(digest) != 0 {
				t.Fatal("a raw sequence notice became an independent business/proof source", err)
			}
			if f.count(`SELECT count(*) FROM native_decoded_events WHERE account_id=$1`, req.AccountID) != 1 ||
				f.count(`SELECT count(*) FROM native_decoded_events WHERE event_id=$1 AND account_id=$2 AND state='decoded'`,
					canonicalID, req.AccountID) != 1 ||
				f.count(`SELECT count(*) FROM native_decoded_events WHERE event_id=$1`, evidenceID) != 0 {
				t.Fatal("sequence supplementation replaced or duplicated the primary decoded event")
			}
			if err := f.db.Platform.QueryRow(f.ctx, `SELECT payload_sealed FROM native_received_events
				WHERE id=$1 AND account_id=$2 AND event_key=$3`, canonicalID, req.AccountID, canonicalKey).
				Scan(&sealed); err != nil {
				t.Fatal(err)
			}
			retained, err := service.Vault.Open("native-event:"+req.AccountID+":"+canonicalKey, sealed)
			unchanged := err == nil && bytes.Equal(retained, canonicalRaw)
			clear(retained)
			if !unchanged {
				t.Fatal("read-only sequence recovery rewrote the original Seq0 payload", err)
			}
		})
	}
}
