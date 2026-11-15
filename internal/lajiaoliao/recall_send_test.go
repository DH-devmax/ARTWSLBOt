package lajiaoliao

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
)

func TestRecallOwnContractFenceAndNoRetry(t *testing.T) {
	writes := 0
	client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
		writes++
		if r.URL.Path != "/msg/revoke_msg" || len(body) != 3 || body["conversationID"] != "sg_string-group" ||
			body["seq"] != float64(17) || body["userID"] != "string-self" {
			t.Fatal("recall used another platform's contract")
		}
		return nil, errors.New("synthetic lost response")
	})
	ref := nativeauth.MessageReference{Platform: "lajiaoliao", EventID: 1, Group: "string-group", Sender: "string-self",
		SourceDigest: strings.Repeat("a", 64), CreatedAt: time.Now().UnixMilli(), Conversation: "sg_string-group", Sequence: 17}
	session := Session{UserID: "string-self", IMToken: "synthetic", DeviceID: "device-fixture"}
	blocked := errors.New("cancelled")
	if err := client.Recall(context.Background(), session, ref, func(context.Context, func(context.Context) error) error { return blocked }); !errors.Is(err, blocked) || writes != 0 {
		t.Fatal("cancelled recall submitted")
	}
	if err := client.Recall(context.Background(), session, ref, func(ctx context.Context, send func(context.Context) error) error { return send(ctx) }); err == nil || writes != 1 {
		t.Fatal("unknown recall retried or fabricated success", writes, err)
	}
	ref.CreatedAt = time.Now().Add(-3 * time.Minute).UnixMilli()
	if err := client.Recall(context.Background(), session, ref, func(ctx context.Context, send func(context.Context) error) error { return send(ctx) }); err == nil || writes != 1 {
		t.Fatal("expired recall submitted")
	}
}
