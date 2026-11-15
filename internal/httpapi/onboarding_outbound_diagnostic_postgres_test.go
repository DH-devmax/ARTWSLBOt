package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"wangshangliao/internal/control"
	"wangshangliao/internal/nativeauth"
)

func TestOnboardingControlledClaimantPostgresRetainsSafeSubmitDiagnosis(t *testing.T) {
	for _, number := range []int{0, 1001} {
		t.Run(fmt.Sprint(number), func(t *testing.T) {
			f := newOnboardingClaimantFixture(t, "wsl", "owner")
			f.openWindow(t)
			prepared := f.prepare(t)
			assertOnboardingOutboundSubmitDiagnostic(t, f, prepared, nil)
			f.outbound.onOutboundEffect = func(context.Context, control.GroupBindingOutboundRequest) error {
				return fmt.Errorf("synthetic-private-token: %w", &nativeauth.Error{
					Code: "native_business_rejected", Number: number, Submitted: true,
					Diagnostic: "synthetic-private-provider-body",
				})
			}
			result := f.send(t, "synthetic-outbound-diagnosis")
			if result.FriendOutbound == nil || result.FriendOutbound.Status != "failed" ||
				result.FriendOutbound.Code != "native_business_rejected" || result.FriendOutbound.CanSend {
				t.Fatal("provider rejection changed its durable outcome or became retryable")
			}
			raw := onboardingAPI(t, f.admin, http.MethodGet, "/group-binding/challenges/"+f.challenge.ID,
				"", nil, http.StatusOK)
			var reloaded control.GroupBindingChallenge
			if err := json.Unmarshal(raw, &reloaded); err != nil || reloaded.FriendOutbound == nil {
				t.Fatal("readback lost the original attempt", err)
			}
			diagnostic := reloaded.FriendOutbound.SubmitDiagnostic
			if number == 0 {
				if diagnostic != nil {
					t.Fatal("submission without a provider number manufactured a diagnosis")
				}
			} else if diagnostic == nil || diagnostic.Version != 1 ||
				diagnostic.ProviderCode != number || diagnostic.Stage != "friend_apply" ||
				diagnostic.Platform != "wsl" || diagnostic.Category != "upstream_rejected" ||
				diagnostic.ObservedAt.IsZero() {
				t.Fatal("persistent safe diagnosis was lost")
			}
			var original *control.GroupBindingOutboundSubmitDiagnostic
			if diagnostic != nil {
				copy := *diagnostic
				original = &copy
			}
			assertOnboardingOutboundSubmitDiagnostic(t, f, result, original)
			assertOnboardingOutboundSubmitDiagnostic(t, f, reloaded, original)
			if strings.Contains(string(raw), "synthetic-private") {
				t.Fatal("provider body or wrapped error entered a public projection")
			}
			replay := f.send(t, "synthetic-outbound-diagnosis")
			assertOnboardingOutboundSubmitDiagnostic(t, f, replay, original)
			if f.outbound.outboundSends.Load() != 1 || f.outbound.outboundEffects.Load() != 1 {
				t.Fatal("readback or duplicate confirmation resent the application")
			}
			f.assertUnproven(t, result)
			f.assertUsage(t, 1)
			updates := []string{
				`UPDATE group_binding_outbound_attempts SET provider_category='upstream_rejected',
						provider_code=1002,provider_observed_at=clock_timestamp() WHERE id=$1`,
			}
			if original != nil {
				updates = append(updates,
					`UPDATE group_binding_outbound_attempts SET provider_category='rate_limited' WHERE id=$1`,
					`UPDATE group_binding_outbound_attempts SET provider_code=1002 WHERE id=$1`,
					`UPDATE group_binding_outbound_attempts
							SET provider_observed_at=provider_observed_at+interval '1 second' WHERE id=$1`,
					`UPDATE group_binding_outbound_attempts SET provider_category=NULL,
							provider_code=NULL,provider_observed_at=NULL WHERE id=$1`)
			}
			for _, update := range updates {
				_, err := f.db.Platform.Exec(context.Background(), update, prepared.FriendOutbound.ID)
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
					t.Fatal("failed history allowed diagnosis replacement or fabrication", err)
				}
				assertOnboardingOutboundSubmitDiagnostic(t, f, f.readChallenge(t, f.challenge), original)
			}
		})
	}
}

func TestOnboardingControlledClaimantPostgresUnknownRetainsSubmitDiagnosisAcrossReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		states []string
	}{
		{"pending", []string{"pending"}},
		{"rejected", []string{"rejected"}},
		{"friends", []string{"friends"}},
		{"pending_then_rejected", []string{"pending", "rejected"}},
		{"pending_then_friends", []string{"pending", "friends"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOnboardingClaimantFixture(t, "wsl", "owner")
			f.openWindow(t)
			prepared := f.prepare(t)
			f.outbound.onOutboundEffect = func(context.Context, control.GroupBindingOutboundRequest) error {
				return fmt.Errorf("synthetic-private-token: %w", &nativeauth.Error{
					Code: "native_transport_unknown", Number: 502, Submitted: true,
					Diagnostic: "synthetic-private-provider-body",
				})
			}
			const sendKey = "synthetic-unknown-diagnosis"
			unknown := f.send(t, sendKey)
			if unknown.FriendOutbound == nil || unknown.FriendOutbound.SubmitDiagnostic == nil {
				t.Fatal("typed unknown outcome lost its submission diagnosis")
			}
			original := *unknown.FriendOutbound.SubmitDiagnostic
			if original.Version != 1 || original.Platform != "wsl" || original.Stage != "friend_apply" ||
				original.Category != "transport_unavailable" || original.ProviderCode != 502 || original.ObservedAt.IsZero() {
				t.Fatal("unknown outcome changed its numeric submission evidence")
			}
			operation := prepared.FriendOutbound.ID
			var sealed []byte
			if err := f.db.Platform.QueryRow(context.Background(), `SELECT payload_sealed
				FROM group_binding_outbound_attempts WHERE id=$1`, operation).Scan(&sealed); err != nil {
				t.Fatal(err)
			}
			var reconciles int32
			assertOutcome := func(out control.GroupBindingChallenge, status, code string) {
				t.Helper()
				unresolved := status == "unknown" || status == "submitted"
				if out.FriendOutbound == nil || out.FriendOutbound.ID != operation ||
					out.FriendOutbound.Status != status || out.FriendOutbound.Code != code || out.FriendOutbound.CanSend ||
					out.FriendOutbound.CanReconcile != unresolved || status == "unknown" && out.CanCancel {
					t.Fatal("read-only observation changed the original attempt or its admission")
				}
				assertOnboardingOutboundSubmitDiagnostic(t, f, out, &original)
				if friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts
					WHERE id=$1 AND challenge_id=$2 AND account_id=$3 AND attempts_used=1
					AND send_key=$4 AND payload_sealed=$5 AND status=$6 AND result_code=$7`,
					operation, f.challenge.ID, f.claimant, sendKey, sealed, status, code) != 1 {
					t.Fatal("observation replaced the sealed attempt or changed its single-use history")
				}
				if f.outbound.outboundSends.Load() != 1 || f.outbound.outboundEffects.Load() != 1 ||
					f.outbound.outboundReconciles.Load() != reconciles {
					t.Fatal("observation retried the submission or performed unexpected recovery reads")
				}
				f.assertUnproven(t, out)
				f.assertUsage(t, 1)
			}
			assertReadbackAndReplay := func(status, code string) {
				t.Helper()
				before := outboundCalls(f.outbound.bindingOutboundNativeFixture)
				assertOutcome(f.readChallenge(t, f.challenge), status, code)
				assertOutcome(f.send(t, sendKey), status, code)
				controlledFriendAPI(t, f.admin, f.challenge, f.window, "friend-send",
					"synthetic-unknown-diagnosis-replacement", "", http.StatusConflict)
				if outboundCalls(f.outbound.bindingOutboundNativeFixture) != before {
					t.Fatal("readback or duplicate confirmation dispatched native work")
				}
			}
			assertOutcome(unknown, "unknown", "native_transport_unknown")
			assertReadbackAndReplay("unknown", "native_transport_unknown")
			for index, state := range tc.states {
				f.outbound.outboundReconcileState = state
				out, err := f.app.GroupBinding.ReconcileOutboundFriend(context.Background(), f.ref, f.challenge.ID,
					operation, control.GroupBindingConfirmation{Version: f.challenge.Version,
						Key: fmt.Sprintf("synthetic-diagnosis-reconcile-%d", index)})
				if err != nil {
					t.Fatal("read-only reconciliation failed", err)
				}
				reconciles++
				status, code := "submitted", "native_friend_request_observed"
				switch state {
				case "rejected":
					status, code = "failed", "native_friend_request_rejected"
				case "friends":
					status, code = "friends", "native_friend_relationship_observed"
				}
				assertOutcome(out, status, code)
				assertReadbackAndReplay(status, code)
			}
		})
	}
}

// The complete fixture verifies old writers against every current scope guard.
func TestOnboardingControlledClaimantPostgresV132SQLDiagnosticCompatibility(t *testing.T) {
	for _, number := range []int{0, 502} {
		t.Run(fmt.Sprint(number), func(t *testing.T) {
			f := newOnboardingClaimantFixture(t, "wsl", "owner")
			f.openWindow(t)
			ctx := context.Background()
			before := outboundCalls(f.outbound.bindingOutboundNativeFixture)
			tx, err := f.db.Platform.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			oldID := control.NewID()
			// Check the v132 column list with current guards, then discard the
			// opaque SQL-only payload before preparing a real fixture request.
			tag, err := tx.Exec(ctx, `INSERT INTO group_binding_outbound_attempts(id,challenge_id,
				actor_id,tenant_id,platform,account_id,account_epoch,account_version,
				applicant_version,challenge_version,gate_version,payload_sealed,prepare_key,
				status,result_code,expires_at,test_window_id)
				SELECT $1,c.id,c.applicant_id,c.tenant_id,c.platform,a.id,a.epoch,a.version,
					c.applicant_version,c.version,0,$5,$6,'prepared','',w.expires_at,w.id
				FROM group_binding_challenges c JOIN accounts a ON a.id=$3
				JOIN test_windows w ON w.id=$4 WHERE c.id=$2`,
				oldID, f.challenge.ID, f.claimant, f.window.ID, bytes.Repeat([]byte{1}, 64),
				"synthetic-v132-sql-prepare")
			if err != nil || tag.RowsAffected() != 1 {
				t.Fatal("v132 INSERT without diagnosis fields became incompatible", err)
			}
			var empty bool
			if err = tx.QueryRow(ctx, `SELECT provider_category IS NULL AND provider_code IS NULL
				AND provider_observed_at IS NULL FROM group_binding_outbound_attempts WHERE id=$1`, oldID).
				Scan(&empty); err != nil || !empty {
				t.Fatal("old INSERT manufactured a submission diagnosis", err)
			}
			if err = tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if outboundCalls(f.outbound.bindingOutboundNativeFixture) != before ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts
					WHERE challenge_id=$1`, f.challenge.ID) != 0 {
				t.Fatal("SQL-only compatibility check persisted work or called native code")
			}
			f.assertUsage(t, 0)
			prepared := f.prepare(t)
			assertOnboardingOutboundSubmitDiagnostic(t, f, prepared, nil)
			f.outbound.onOutboundEffect = func(context.Context, control.GroupBindingOutboundRequest) error {
				return &nativeauth.Error{Code: "native_transport_unknown", Number: number, Submitted: true}
			}
			const sendKey = "synthetic-v132-sql-confirm"
			unknown := f.send(t, sendKey)
			if unknown.FriendOutbound == nil || unknown.FriendOutbound.Status != "unknown" ||
				unknown.FriendOutbound.ID != prepared.FriendOutbound.ID {
				t.Fatal("compatibility fixture lost its single unknown operation")
			}
			var original *control.GroupBindingOutboundSubmitDiagnostic
			if number != 0 {
				diagnostic := unknown.FriendOutbound.SubmitDiagnostic
				if diagnostic == nil || diagnostic.Version != 1 || diagnostic.Platform != "wsl" ||
					diagnostic.Stage != "friend_apply" || diagnostic.Category != "transport_unavailable" ||
					diagnostic.ProviderCode != number || diagnostic.ObservedAt.IsZero() {
					t.Fatal("compatibility fixture omitted its original numeric diagnosis")
				}
				copy := *diagnostic
				original = &copy
			}
			assertOnboardingOutboundSubmitDiagnostic(t, f, unknown, original)
			for _, result := range []struct{ status, code string }{
				{"submitted", "native_friend_request_observed"},
				{"failed", "native_friend_request_rejected"},
			} {
				// This is the unchanged v132 reconciliation UPDATE: it neither
				// reads nor writes any of the added diagnosis columns.
				tag, err = f.db.Platform.Exec(ctx, `UPDATE group_binding_outbound_attempts
					SET status=$2,result_code=$3,updated_at=clock_timestamp()
					WHERE id=$1 AND status IN ('submitted','unknown')`,
					prepared.FriendOutbound.ID, result.status, result.code)
				if err != nil || tag.RowsAffected() != 1 {
					t.Fatal("v132 UPDATE was rejected by the new diagnosis guard", err)
				}
				out := f.readChallenge(t, f.challenge)
				if out.FriendOutbound == nil || out.FriendOutbound.ID != prepared.FriendOutbound.ID ||
					out.FriendOutbound.Status != result.status || out.FriendOutbound.Code != result.code ||
					out.FriendOutbound.CanSend {
					t.Fatal("old writer changed the operation or re-enabled a submission")
				}
				assertOnboardingOutboundSubmitDiagnostic(t, f, out, original)
				before = outboundCalls(f.outbound.bindingOutboundNativeFixture)
				replay := f.send(t, sendKey)
				assertOnboardingOutboundSubmitDiagnostic(t, f, replay, original)
				if replay.FriendOutbound.ID != out.FriendOutbound.ID ||
					replay.FriendOutbound.Status != result.status || replay.FriendOutbound.CanSend ||
					outboundCalls(f.outbound.bindingOutboundNativeFixture) != before ||
					f.outbound.outboundSends.Load() != 1 || f.outbound.outboundEffects.Load() != 1 ||
					f.outbound.outboundReconciles.Load() != 0 {
					t.Fatal("old-writer readback or replay dispatched another native operation")
				}
				f.assertUnproven(t, out)
				f.assertUnproven(t, replay)
				f.assertUsage(t, 1)
			}
			_, err = f.db.Platform.Exec(ctx, `UPDATE group_binding_outbound_attempts
				SET provider_category='upstream_rejected',provider_code=1001,
					provider_observed_at=clock_timestamp() WHERE id=$1`, prepared.FriendOutbound.ID)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatal("old failed history allowed a new or replacement diagnosis", err)
			}
			assertOnboardingOutboundSubmitDiagnostic(t, f, f.readChallenge(t, f.challenge), original)
			f.assertUsage(t, 1)
		})
	}
}

func TestOnboardingControlledClaimantPostgresForwardDiagnosisPreservesExistingRows(t *testing.T) {
	integrationDB(t)
	ctx := context.Background()
	input, err := os.ReadFile("../../.env")
	if err != nil {
		t.Fatal("isolated migration configuration unavailable")
	}
	var ownerURL *url.URL
	for _, line := range strings.Split(string(input), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok && key == "MIGRATION_DATABASE_URL" {
			ownerURL, err = url.Parse(value)
			break
		}
	}
	if err != nil || ownerURL == nil || ownerURL.Hostname() != "127.0.0.1" {
		t.Fatal("migration test requires explicit localhost configuration")
	}
	ownerURL.Path = "/wsl_test"
	if os.Getenv("WSL_CONTAINER_TEST") == "1" {
		ownerURL.Host = "postgres:5432"
	}
	conn, err := pgx.Connect(ctx, ownerURL.String())
	if err != nil {
		t.Fatal("isolated migration connection unavailable")
	}
	defer conn.Close(ctx)
	var databaseName string
	if err = conn.QueryRow(ctx, "SELECT current_database()").Scan(&databaseName); err != nil || databaseName != "wsl_test" {
		t.Fatal("refusing migration compatibility outside wsl_test")
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// Apply the actual new migration to a transaction-local pre-072 shape.
	// No production schema, history, trigger or migration record is changed.
	schema := pgx.Identifier{"diagnosis_" + strings.ReplaceAll(control.NewID(), "-", "")}.Sanitize()
	if _, err = tx.Exec(ctx, "CREATE SCHEMA "+schema+"; SET LOCAL search_path TO "+schema); err != nil {
		t.Fatal("cannot prepare the isolated migration schema", err)
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE group_binding_outbound_attempts(
		id uuid PRIMARY KEY,attempts_used integer NOT NULL,status text NOT NULL,result_code text NOT NULL);
		INSERT INTO group_binding_outbound_attempts VALUES
		('11111111-1111-4111-8111-111111111111',1,'failed','native_business_rejected'),
		('22222222-2222-4222-8222-222222222222',1,'unknown','native_transport_unknown')`); err != nil {
		t.Fatal("cannot prepare the isolated prior-row migration fixture", err)
	}
	migration, err := os.ReadFile("../database/migrations/072_onboarding_outbound_diagnostic.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, string(migration)); err != nil {
		t.Fatal("forward diagnosis migration rejected existing old rows", err)
	}
	var preserved int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM group_binding_outbound_attempts
		WHERE provider_code IS NULL AND provider_category IS NULL AND provider_observed_at IS NULL
		AND attempts_used=1 AND ((id='11111111-1111-4111-8111-111111111111' AND status='failed'
			AND result_code='native_business_rejected')
		OR (id='22222222-2222-4222-8222-222222222222' AND status='unknown'
			AND result_code='native_transport_unknown'))`).Scan(&preserved); err != nil || preserved != 2 {
		t.Fatal("forward migration changed or backfilled historical submissions", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE group_binding_outbound_attempts SET status='submitted',
		result_code='native_friend_request_observed' WHERE id='22222222-2222-4222-8222-222222222222'`); err != nil {
		t.Fatal("old reconciliation SQL became incompatible after migration", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO group_binding_outbound_attempts(id,attempts_used,status,result_code)
		VALUES('33333333-3333-4333-8333-333333333333',0,'prepared','');
		UPDATE group_binding_outbound_attempts SET attempts_used=1,status='submitting'
			WHERE id='33333333-3333-4333-8333-333333333333';
		UPDATE group_binding_outbound_attempts SET status='unknown',result_code='native_transport_unknown',
			provider_code=502,provider_category='transport_unavailable',provider_observed_at=clock_timestamp()
			WHERE id='33333333-3333-4333-8333-333333333333'`); err != nil {
		t.Fatal("new writer failed after forward migration", err)
	}
}

func assertOnboardingOutboundSubmitDiagnostic(t *testing.T, f *onboardingClaimantFixture,
	out control.GroupBindingChallenge, expected *control.GroupBindingOutboundSubmitDiagnostic) {
	t.Helper()
	if out.ID != f.challenge.ID || out.FriendOutbound == nil {
		t.Fatal("diagnosis lost its parent challenge or outbound attempt")
	}
	got := out.FriendOutbound.SubmitDiagnostic
	if expected == nil {
		if got != nil || friendCount(t, f.onboardingFixture, `SELECT count(*)
			FROM group_binding_outbound_attempts WHERE id=$1 AND challenge_id=$2
			AND provider_category IS NULL AND provider_code IS NULL AND provider_observed_at IS NULL`,
			out.FriendOutbound.ID, f.challenge.ID) != 1 {
			t.Fatal("missing submission evidence was backfilled")
		}
		return
	}
	if got == nil || got.Version != expected.Version || got.Platform != expected.Platform ||
		got.Stage != expected.Stage || got.Category != expected.Category ||
		got.ProviderCode != expected.ProviderCode || !got.ObservedAt.Equal(expected.ObservedAt) {
		t.Fatal("readback or result transition changed the immutable submission diagnosis")
	}
	if friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts
		WHERE id=$1 AND challenge_id=$2 AND provider_category=$3 AND provider_code=$4 AND provider_observed_at=$5`,
		out.FriendOutbound.ID, f.challenge.ID, expected.Category, expected.ProviderCode, expected.ObservedAt) != 1 {
		t.Fatal("persistent submission diagnosis differs from its original projection")
	}
}
