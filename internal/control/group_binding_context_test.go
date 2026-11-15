package control

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestBindingCreateTargetComesFromWebsiteRole(t *testing.T) {
	tenant, other := NewID(), NewID()
	operator := Actor{ID: NewID(), Role: "superadmin"}
	for _, target := range []string{"", "not-a-tenant"} {
		_, _, err := bindingCreateTarget(operator, target)
		capabilityError(t, err, "group_binding_customer_required")
	}
	got, mode, err := bindingCreateTarget(operator, tenant)
	if err != nil || got != tenant || mode != "operator_demo" || operator.TenantID != "" {
		t.Fatal("operator was impersonated as its resource customer", got, mode, err)
	}
	customer := Actor{ID: NewID(), TenantID: tenant, Role: "user"}
	for _, target := range []string{tenant, other} {
		_, _, err = bindingCreateTarget(customer, target)
		capabilityError(t, err, "group_binding_target_forbidden")
	}
	got, mode, err = bindingCreateTarget(customer, "")
	if err != nil || got != tenant || mode != "tenant_business" {
		t.Fatal("legacy customer request changed its resource tenant", got, mode, err)
	}
	_, _, err = bindingCreateTarget(Actor{ID: NewID(), Role: "user"}, "")
	capabilityError(t, err, "tenant_owner_required")
}

func TestBindingRecordDoesNotChangeApplicantOrMode(t *testing.T) {
	tenant, applicant := NewID(), NewID()
	c := bindingRecord{Tenant: tenant, Applicant: applicant, Platform: "lajiaoliao", Mode: "operator_demo"}
	operator := Actor{ID: applicant, Role: "superadmin"}
	if !bindingRecordMatches(c, operator, c.Platform) {
		t.Fatal("same operator cannot resume the selected customer's challenge")
	}
	for _, actor := range []Actor{
		{ID: NewID(), Role: "superadmin"},
		{ID: applicant, Role: "user", TenantID: tenant},
		{ID: applicant, Role: "user", TenantID: NewID()},
	} {
		if bindingRecordMatches(c, actor, c.Platform) {
			t.Fatal("another actor or changed role resumed an operator challenge")
		}
	}
	if bindingRecordMatches(c, operator, "wsl") {
		t.Fatal("challenge crossed platforms")
	}
	c.Mode = "tenant_business"
	if bindingRecordMatches(c, operator, c.Platform) {
		t.Fatal("promoted account inherited a customer-mode submission")
	}
	customer := Actor{ID: applicant, Role: "user", TenantID: tenant}
	if !bindingRecordMatches(c, customer, c.Platform) {
		t.Fatal("unchanged customer scope rejected")
	}
	customer.TenantID = NewID()
	if bindingRecordMatches(c, customer, c.Platform) {
		t.Fatal("website actor changed the persisted resource tenant")
	}
	c.Mode = ""
	if bindingRecordMatches(c, customer, c.Platform) {
		t.Fatal("empty mode acquired a fallback")
	}
}

func TestBindingContextRejectsUnboundedOrCrossPlatformBeforeDatabase(t *testing.T) {
	service := GroupBindingService{}
	for _, in := range []struct {
		platform, view, search string
		offset, limit          int
	}{
		{"wsl", "lajiaoliao", "", 0, 20},
		{"maibot", "maibot", "", 0, 20},
		{"wsl", "wsl", "", -1, 20},
		{"wsl", "wsl", "", 0, 51},
		{"wsl", "wsl", "", 0, 0},
		{"wsl", "wsl", "", 5000001, 20},
		{"wsl", "wsl", "\n", 0, 20},
		{"wsl", "wsl", string([]byte{0xff}), 0, 20},
	} {
		_, err := service.Context(context.Background(), SessionRef{}.InPlatform(in.view),
			in.platform, in.search, in.offset, in.limit)
		capabilityError(t, err, "group_binding_context_invalid")
	}
}

func TestBindingTargetParticipatesInImmutableCreateDigest(t *testing.T) {
	in := GroupBindingCreate{Platform: "wsl", Key: "transport-request-key"}
	legacy, _ := json.Marshal(struct {
		Platform       string `json:"platform"`
		RestoreGroupID string `json:"restore_group_id,omitempty"`
		BindingVersion int64  `json:"binding_version,omitempty"`
	}{Platform: in.Platform})
	if !bytes.Equal(bindingCreateDigest(in), bindingTokenDigest(string(legacy))) {
		t.Fatal("optional target changed old customer idempotency hashes")
	}
	in.TargetTenantID = NewID()
	first := bindingCreateDigest(in)
	in.Key = "another-transport-request-key"
	if !bytes.Equal(first, bindingCreateDigest(in)) {
		t.Fatal("transport key changed a stable onboarding request")
	}
	in.TargetTenantID = NewID()
	if bytes.Equal(first, bindingCreateDigest(in)) {
		t.Fatal("same idempotency key could substitute another resource customer")
	}
}

type bindingCapacityTx struct {
	pgx.Tx
	query *capabilityQueries
}

func (tx bindingCapacityTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return tx.query.QueryRow(ctx, sql, args...)
}

func TestBindingOperatorCommercialExemptionRetainsReservations(t *testing.T) {
	for _, tc := range []struct {
		name, mode, code string
		rows             []capabilityRow
	}{
		{"operator-no-subscription-read", "operator_demo", "", []capabilityRow{
			{values: []any{10, 10, 10}}, {values: []any{1, 1, 1}},
		}},
		{"operator-global-reservation", "operator_demo", "group_quota_exceeded", []capabilityRow{
			{values: []any{0, 299, 0}}, {values: []any{0, 1, 0}},
		}},
		{"operator-account-reservation", "operator_demo", "account_group_limit_reached", []capabilityRow{
			{values: []any{0, 29, 29}}, {values: []any{0, 1, 1}},
		}},
		{"customer-retains-commercial-limit", "tenant_business", "group_quota_exceeded", []capabilityRow{
			{values: []any{1}}, {values: []any{1, 1, 1}}, {values: []any{0, 0, 0}},
		}},
		{"empty-mode", "", "group_binding_scope_changed", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &capabilityQueries{t: t, rows: tc.rows}
			err := bindingCapacityMode(context.Background(), bindingCapacityTx{query: q},
				NewID(), "wsl", NewID(), "", "", tc.mode)
			if tc.code == "" && err != nil {
				t.Fatal(err)
			}
			if tc.code != "" {
				capabilityError(t, err, tc.code)
			}
			if q.calls != len(tc.rows) {
				t.Fatal("admission skipped reservation reads or performed an extra commercial read", q.calls)
			}
		})
	}
}
