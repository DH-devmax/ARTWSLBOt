package httpapi

import (
	"context"
	"testing"

	"wangshangliao/internal/control"
)

func TestGroupBindingOutboundPostgresMobileSessionFence(t *testing.T) {
	for _, change := range []string{"none", "revoked", "session-version", "deployment-version", "fresh-evidence"} {
		t.Run(change, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, "wsl")
			ctx := context.Background()
			account := f.accounts["wsl"]
			if _, err := f.db.Platform.Exec(ctx, `INSERT INTO native_mobile_deployment(singleton,sealed)
				VALUES(true,'synthetic-deployment'::bytea)`); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.Platform.Exec(ctx, `INSERT INTO native_mobile_sessions
				(account_id,native_uid,account_epoch,deployment_version,credential_sealed,state)
				SELECT id,native_uid,epoch,1,'synthetic-mobile'::bytea,'authenticated'
				FROM accounts WHERE id=$1`, account); err != nil {
				t.Fatal(err)
			}
			n.outboundEvidence = func(e *control.GroupBindingOutboundEvidence) {
				e.MobileDeploymentVersion, e.MobileSessionVersion = 1, 1
			}
			c := outboundControlledChallenge(t, f, client, "wsl", "mobile-fence-"+change)
			c = outboundPrepare(t, f, n, ref, c, "mobile-prepare-"+change)
			outboundOpenGate(t, f, "wsl")
			n.beforeOutboundSend = func(ctx context.Context, _ control.GroupBindingOutboundRequest) error {
				switch change {
				case "revoked":
					_, err := f.db.Platform.Exec(ctx, `UPDATE native_mobile_sessions
						SET state='revoked',credential_sealed=NULL,version=version+1 WHERE account_id=$1`, account)
					return err
				case "session-version":
					_, err := f.db.Platform.Exec(ctx, `UPDATE native_mobile_sessions
						SET version=version+1 WHERE account_id=$1`, account)
					return err
				case "deployment-version":
					_, err := f.db.Platform.Exec(ctx, `UPDATE native_mobile_deployment
						SET version=version+1 WHERE singleton`)
					return err
				case "fresh-evidence":
					n.outboundEvidence = func(e *control.GroupBindingOutboundEvidence) {
						e.MobileDeploymentVersion, e.MobileSessionVersion = 1, 2
					}
				}
				return nil
			}
			result := outboundSend(t, f.app.GroupBinding, ref, c, "mobile-send-"+change)
			if change == "none" {
				if n.outboundEffects.Load() != 1 || result.FriendOutbound.Status != "submitted" {
					t.Fatal("valid mobile proof did not submit once", result.FriendOutbound.Status)
				}
			} else if n.outboundEffects.Load() != 0 || result.FriendOutbound.Status != "failed" {
				t.Fatal("stale mobile proof reached native submission", result.FriendOutbound.Status)
			}
			before := outboundCalls(n)
			outboundSend(t, f.app.GroupBinding, ref, c, "mobile-send-"+change)
			if outboundCalls(n) != before {
				t.Fatal("duplicate confirmation repeated a mobile lookup or submission")
			}
		})
	}
}
