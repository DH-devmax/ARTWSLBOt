package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

func TestGroupBindingPostgresTransferHidesReceivedHistory(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			ctx := context.Background()
			account := f.accounts[platform]
			alice, bob := f.alice, f.bob
			alice.workspace, bob.workspace = platform, platform
			nativeGroup := "transfer-history-group"
			if platform == "wsl" {
				nativeGroup = "123"
			}
			first := f.createChallenge(t, alice, platform, "history-source-challenge")
			first = f.prove(t, alice, platform, first, f.native.owner, nativeGroup)
			onboardingAPI(t, alice, "POST", "/group-binding/challenges/"+first.ID+"/confirm", "history-source-confirm",
				map[string]any{"version": first.Version}, 200)
			directory, _ := json.Marshal([]nativeauth.DirectoryGroup{{ID: nativeGroup, Team: "456", Name: "History group"}})
			sealed, err := f.vault.Seal("native-directory:"+account, directory)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Platform.Exec(ctx, `INSERT INTO native_directory_snapshots(account_id,payload_sealed,sampled_at)
				VALUES($1,$2,clock_timestamp())`, account, sealed); err != nil {
				t.Fatal(err)
			}
			reader := gateway.NewReceiverManager(f.db.Platform, f.vault, f.app.nativeRequestIDs)
			conversation, err := reader.ChatConversation(ctx, platform, account, nativeGroup)
			if err != nil {
				t.Fatal(err)
			}
			old := bindingHistoryMessage(t, f, platform, nativeGroup, conversation, "tenant-history-before",
				time.Now().Add(-30*time.Second))
			base := "/accounts/" + account + "/conversations/" + conversation + "/messages"
			media := base + "/" + strconv.FormatInt(old, 10) + "/media/sender"
			admin := f.admin
			admin.workspace = platform
			if data := expect(t, admin, "GET", base, nil, 200); !bytes.Contains(data, []byte("tenant-history-before")) {
				t.Fatal("the pre-transfer control message was not readable")
			}
			var picture bytes.Buffer
			if err = png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
				t.Fatal(err)
			}
			var downloads atomic.Int32
			transport := projectionMediaTransport(func(r *http.Request) (*http.Response, error) {
				downloads.Add(1)
				return &http.Response{StatusCode: 200, Header: http.Header{},
					Body: io.NopCloser(bytes.NewReader(picture.Bytes())), Request: r}, nil
			})
			f.app.mediaClient = &http.Client{Transport: transport}
			f.app.chiliMediaClient = &http.Client{Transport: transport}
			if platform == "wsl" {
				key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{29}, 32))
				deployment := nativeauth.Deployment{Origin: "https://ifeng-api01.hfjtqy.com",
					ProtocolVersion: "2.8.3", DesktopPlatform: "macos", Metadata: make([]uint64, 14),
					SigningSeed: key, BodyKey: key, ServerKey: key, CaptchaID: "synthetic",
					DeviceID: "synthetic-transfer-history"}
				raw, _ := json.Marshal(deployment)
				sealed, err = f.vault.Seal("native-deployment:v1", raw)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.db.Platform.Exec(ctx, `UPDATE native_deployment SET sealed=$1 WHERE singleton`, sealed); err != nil {
					t.Fatal(err)
				}
			}
			assertNativeImage(t, admin, media, picture.Bytes())
			if platform == "lajiaoliao" {
				rows, err := reader.MessageCandidates(ctx, platform, account, nativeGroup, "tenant-history")
				if err != nil || len(rows) != 1 || rows[0].Reference.EventID != old {
					t.Fatal("pre-transfer candidate control did not contain the retained message", rows, err)
				}
			}

			next := f.createChallenge(t, bob, platform, "history-target-challenge")
			next = f.prove(t, bob, platform, next, f.native.owner, nativeGroup)
			onboardingAPI(t, bob, "POST", "/group-binding/challenges/"+next.ID+"/confirm", "history-target-confirm",
				map[string]any{"version": next.Version}, 200)
			var review control.GroupBindingChallenge
			raw := onboardingAPI(t, admin, "GET", "/group-binding/challenges/"+next.ID, "", nil, 200)
			if err = json.Unmarshal(raw, &review); err != nil || review.Transfer == nil {
				t.Fatal("explicit history transfer review missing", err)
			}
			onboardingAPI(t, admin, "POST", "/group-binding/challenges/"+next.ID+"/transfer-review", "history-explicit-transfer",
				map[string]any{"version": review.Version, "approve": true, "source_tenant_id": f.tenantA,
					"binding_version": review.Transfer.BindingVersion}, 200)
			expect(t, bob, "GET", base, nil, 401)
			bob = f.client(t, f.bob.base, f.bobID)
			bob.workspace = platform
			beforeDownloads := downloads.Load()
			for _, path := range []string{base, base + "?ids=" + strconv.FormatInt(old, 10),
				media, "/accounts/" + account + "/conversations"} {
				expect(t, bob, "GET", path, nil, 403)
			}
			if downloads.Load() != beforeDownloads {
				t.Fatal("a new tenant's old attachment request reached the media transport")
			}
			for _, table := range []string{"native_received_events", "native_decoded_events"} {
				err = f.db.ActorTenantTx(ctx, f.tenantB, f.bobID, func(tx pgx.Tx) error {
					var count int
					return tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count)
				})
				if err == nil {
					t.Fatal("the new tenant database role could read shared retained data", table)
				}
			}
			if platform == "lajiaoliao" {
				rows, err := reader.MessageCandidates(ctx, platform, account, nativeGroup, "tenant-history")
				if err != nil || len(rows) != 0 {
					t.Fatal("the transferred group's candidate reader disclosed pre-transfer content", rows, err)
				}
				current := bindingHistoryMessage(t, f, platform, nativeGroup, conversation, "tenant-history-after", time.Now())
				rows, err = reader.MessageCandidates(ctx, platform, account, nativeGroup, "tenant-history")
				if err != nil || len(rows) != 1 || rows[0].Reference.EventID != current || rows[0].Preview != "tenant-history-after" {
					t.Fatal("the transferred candidate reader did not isolate the new history", rows, err)
				}
			}
			if data := expect(t, admin, "GET", base+"?ids="+strconv.FormatInt(old, 10), nil, 200); !bytes.Contains(data, []byte("tenant-history-before")) {
				t.Fatal("transfer deleted or rewrote the original received evidence")
			}
		})
	}
}

func bindingHistoryMessage(t *testing.T, f *onboardingFixture, platform, group, conversation, text string, at time.Time) int64 {
	t.Helper()
	account, client := f.accounts[platform], control.NewID()
	message := gateway.DecodedMessage{Platform: platform, NativeSender: "history-sender", NativeTarget: group,
		TransportPeer: "history-sender", TransportTo: group, Message: nativewire.ApplicationMessage{
			Session: 2, ClientID: client, CreatedAt: uint64(at.UnixMilli()), Text: text,
			Sender: nativewire.MessageSource{ID: 7, Avatar: "123456.jpg"}, Target: nativewire.MessageSource{ID: 123}}}
	var raw []byte
	var err error
	if platform == "lajiaoliao" {
		content, _ := json.Marshal(map[string]string{"content": text})
		raw, err = proto.Marshal(&sdkws.MsgData{SendID: "history-sender", RecvID: group, GroupID: group,
			ClientMsgID: client, SessionType: 3, MsgFrom: 100, ContentType: 101, Content: content,
			SendTime: at.UnixMilli(), Seq: 1, SenderFaceURL: "https://lj-im-chat-01.lajiaoliao.com/synthetic-history-avatar"})
	} else {
		message.TransportPeer, message.TransportTo, message.ServerID = "777", "456", "synthetic-history-server"
		raw, err = json.Marshal(nativewire.IMMessage{Scene: 1, Target: message.TransportTo, Sender: message.TransportPeer,
			ServerID: message.ServerID, Fields: map[uint32]string{7: strconv.FormatInt(at.UnixMilli(), 10), 11: client}})
	}
	if err != nil {
		t.Fatal(err)
	}
	source, err := f.vault.Seal("native-event:"+account+":"+client, raw)
	if err != nil {
		t.Fatal(err)
	}
	var event int64
	if err = f.db.Platform.QueryRow(context.Background(), `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
		VALUES($1,$2,'group',$3,$4) RETURNING id`, account, client, at, source).Scan(&event); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(message)
	projection, err := f.vault.Seal(gateway.DecodedAAD(account, event), raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Platform.Exec(context.Background(), `INSERT INTO native_decoded_events(event_id,account_id,state,scene,
		conversation_key,deployment_version,codec_version,payload_sealed,received_at,decoded_at)
		VALUES($1,$2,'decoded','group',$3,1,7,$4,$5,clock_timestamp())`,
		event, account, conversation, projection, at); err != nil {
		t.Fatal(err)
	}
	return event
}
