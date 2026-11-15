package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"wangshangliao/internal/control"
	"wangshangliao/internal/nativeauth"
)

func TestGroupBindingOutboundPostgresFailedLookupIsSafeAndHasNoFriendEffect(t *testing.T) {
	f, n, client, ref, c, window := onboardingControlledFixture(t, "wsl")
	n.beforeOutboundLookup = func(context.Context, control.GroupBindingOutboundLookup) error {
		return bindingOutboundLookupError(&nativeauth.OnboardingLookupError{Stage: "search",
			Cause: &nativeauth.Error{Code: "native_business_rejected", Number: -1,
				Diagnostic: "provider_reason_unrecognized", Submitted: true}})
	}
	status, data, msg := client.requestRaw("POST", "/test-tools/onboarding-friend/challenges/"+c.ID+"/friend-target",
		control.OnboardingControlledTargetInput{Window: window.ID, Version: c.Version, Account: n.target.AddAccount},
		http.Header{"Idempotency-Key": {"lookup-safe-diagnostic-target"}})
	var result struct {
		Diagnostic control.GroupBindingLookupDiagnostic `json:"lookup_diagnostic"`
	}
	if status != 409 || msg != "group_binding_outbound_rejected" || json.Unmarshal(data, &result) != nil ||
		result.Diagnostic.ProviderCode != -1 || result.Diagnostic.Stage != "search" ||
		result.Diagnostic.Category != "upstream_rejected" || result.Diagnostic.Reason != "provider_reason_unrecognized" {
		t.Fatal("controlled route lost its safe lookup diagnosis", status, msg)
	}
	var detail []byte
	if err := f.db.Platform.QueryRow(context.Background(), `SELECT detail FROM audit_events
		WHERE action='group_binding_outbound_lookup_failed' AND resource_id=$1`, c.ID).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{n.target.AddAccount, n.target.NativeID, n.target.Name, c.PrivateChallenge, c.GroupChallenge} {
		if secret != "" && strings.Contains(string(detail), secret) {
			t.Fatal("lookup diagnostic audit recorded a native query, identity or proof")
		}
	}
	if n.outboundLookups.Load() != 1 || n.outboundSends.Load() != 0 || n.outboundEffects.Load() != 0 ||
		friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts WHERE challenge_id=$1`, c.ID) != 0 {
		t.Fatal("lookup failure submitted, retried or created a target attempt")
	}
	before := outboundCalls(n)
	if _, err := f.app.GroupBinding.Read(context.Background(), ref, c.ID); err != nil {
		t.Fatal(err)
	}
	if outboundCalls(n) != before {
		t.Fatal("reading failure progress retried upstream")
	}
}

func TestGroupBindingOutboundPostgresFailedLookupRechecksScopeBeforeDiagnostic(t *testing.T) {
	for _, change := range []string{"cancel", "version", "epoch", "actor"} {
		t.Run(change, func(t *testing.T) {
			f, n, _, ref, c, _ := onboardingControlledFixture(t, "wsl")
			n.afterOutboundLookup = func(ctx context.Context, _ control.GroupBindingOutboundLookup) error {
				var err error
				switch change {
				case "cancel":
					_, err = f.app.GroupBinding.Cancel(ctx, ref, c.ID, c.Version)
				case "version":
					_, err = f.db.Platform.Exec(ctx, `UPDATE group_binding_challenges SET version=version+1 WHERE id=$1`, c.ID)
				case "epoch":
					_, err = f.db.Platform.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, f.accounts["wsl"])
				case "actor":
					_, err = f.db.Platform.Exec(ctx, `UPDATE users SET access_version=access_version+1 WHERE id=$1`, c.Scope.ActorID)
				}
				if err != nil {
					return err
				}
				return bindingOutboundLookupError(&nativeauth.OnboardingLookupError{Stage: "friend_info",
					Cause: &nativeauth.Error{Code: "native_business_rejected", Number: -1,
						Diagnostic: "provider_reason_unrecognized", Submitted: true}})
			}
			_, err := (outboundControlledService{f.app.GroupBinding}).PrepareOutboundFriend(context.Background(), ref, c.ID,
				control.GroupBindingFriendTargetInput{Version: c.Version, Account: n.target.AddAccount, Key: "lookup-late-target"})
			var diagnostic *control.GroupBindingLookupFailure
			if err == nil || errors.As(err, &diagnostic) ||
				friendCount(t, f, `SELECT count(*) FROM audit_events
					WHERE action='group_binding_outbound_lookup_failed' AND resource_id=$1`, c.ID) != 0 ||
				n.outboundLookups.Load() != 1 || n.outboundSends.Load() != 0 {
				t.Fatal("stale website/native scope received or persisted a lookup diagnosis", err)
			}
		})
	}
}
