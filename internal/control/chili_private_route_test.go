package control

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"wangshangliao/internal/security"
)

func TestChiliPrivateRouteSourceFailuresStayClosed(t *testing.T) {
	vault, err := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{71}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	service := &NativeTextService{Vault: vault}
	req := TextRequest{AccountID: NewID(), AccountEpoch: 1, PrivateEventID: 1,
		PrivateConversation: controlledProofDigest([]byte("synthetic-private-route"))}
	databaseFailure := errors.New("synthetic database unavailable")
	for _, mode := range []string{"database", "cancelled", "vault"} {
		for _, notice := range []bool{false, true} {
			ctx, cancel := context.WithCancel(context.Background())
			query := &capabilityQueries{t: t}
			switch mode {
			case "database":
				query.rows = []capabilityRow{{err: databaseFailure}}
			case "cancelled":
				cancel()
			case "vault":
				sealed, sealErr := vault.Seal("different-account-and-event", []byte("synthetic source"))
				if sealErr != nil {
					t.Fatal(sealErr)
				}
				query.rows = []capabilityRow{{values: []any{sealed, "synthetic-event"}}}
			}
			tx := bindingCapacityTx{query: query}
			if notice {
				_, _, err = service.chiliPrivateNoticeSource(ctx, tx, req, "synthetic-self")
			} else {
				_, _, err = service.chiliSource(ctx, tx, req, 1, "private", req.PrivateConversation)
			}
			cancel()
			var classified *Failure
			if err == nil || errors.As(err, &classified) {
				t.Fatalf("%s notice=%t converted a non-classification failure into a skippable source", mode, notice)
			}
			if mode == "database" && !errors.Is(err, databaseFailure) ||
				mode == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("%s notice=%t lost its underlying failure", mode, notice)
			}
		}
	}
}
