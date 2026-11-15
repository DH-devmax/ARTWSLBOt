package control

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/features"
)

type capabilityRow struct {
	values []any
	err    error
}

func (r capabilityRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("synthetic column mismatch")
	}
	for i := range dest {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(r.values[i]))
	}
	return nil
}

type capabilityQueries struct {
	t     *testing.T
	rows  []capabilityRow
	calls int
}

func (q *capabilityQueries) QueryRow(ctx context.Context, _ string, _ ...any) pgx.Row {
	q.t.Helper()
	if err := ctx.Err(); err != nil {
		return capabilityRow{err: err}
	}
	if q.calls >= len(q.rows) {
		q.t.Fatal("unexpected synthetic database read")
	}
	row := q.rows[q.calls]
	q.calls++
	return row
}

func capabilityError(t *testing.T, err error, code string) {
	t.Helper()
	var denied *Failure
	if !errors.As(err, &denied) || denied.Code != code {
		t.Fatalf("got %v, want %s", err, code)
	}
}

func ordinaryControlledBindingRow() capabilityRow {
	return capabilityRow{values: []any{"authenticated_message", "", "", true, true}}
}

func TestCapabilityCatalogAndWorkflowTemplates(t *testing.T) {
	catalog := BusinessPermissionCatalog()
	seen := map[string]bool{}
	for _, spec := range catalog {
		if seen[spec.Code] {
			t.Fatal("duplicate permission", spec.Code)
		}
		seen[spec.Code] = true
	}
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, action := range features.BusinessActions(platform) {
			if !slices.ContainsFunc(catalog, func(p PermissionSpec) bool {
				return p.Code == action.Permission && p.Module == action.Module
			}) {
				t.Fatal("action permission diverged from action catalog", platform, action.ID)
			}
		}
	}
	if d, _ := features.FindAssistantAction("wsl", "group_info"); d.Module != "moderation" {
		t.Fatal("group information uses wrong package module")
	}
	for _, template := range AssistantPermissionTemplates() {
		for _, permission := range template.Permissions {
			if strings.HasPrefix(permission, "group_action:") {
				t.Fatal("workflow template implicitly grants a native action", template.Code)
			}
		}
	}
	catalog[0].Code = "changed"
	if BusinessPermissionCatalog()[0].Code == "changed" {
		t.Fatal("caller changed shared permission catalog")
	}
}

func TestTenantCeilingClosesLegacyAliasesAndMissingRows(t *testing.T) {
	ctx, tenant := context.Background(), NewID()
	for _, row := range []capabilityRow{{err: pgx.ErrNoRows}, {values: []any{[]string{}}}} {
		q := &capabilityQueries{t: t, rows: []capabilityRow{row}}
		capabilityError(t, RequireTenantCapability(ctx, q, tenant, "groups:read"), "tenant_capability_denied")
	}
	grants := []string{"groups:moderation", "feature:blocked_words:enabled", "feature:blocked_words:words"}
	if ceilingAllows(grants, "groups:moderation") {
		t.Fatal("module alias reopened withheld blacklist/exemption fields")
	}
	for _, d := range features.Catalog() {
		if d.Legacy == "groups:moderation" {
			for _, field := range d.Fields {
				grants = append(grants, features.Permission(d.ID, field.Key))
			}
		}
	}
	if !ceilingAllows(grants, "groups:moderation") || !ceilingAllows(grants, "feature:blocked_words:words") {
		t.Fatal("explicit ceiling is not usable")
	}
	q := &capabilityQueries{t: t}
	capabilityError(t, RequireTenantCapability(ctx, q, "", "groups:read"), "tenant_capability_denied")
	capabilityError(t, RequireTenantCapability(ctx, q, tenant, "tests:manage"), "tenant_capability_denied")
	dbErr := errors.New("synthetic database unavailable")
	q.rows = []capabilityRow{{err: dbErr}}
	if !errors.Is(RequireTenantCapability(ctx, q, tenant, "groups:read"), dbErr) {
		t.Fatal("database error became a permissive default")
	}
}

func TestEffectivePermissionsKeepLegacyAliasesWithoutNewFields(t *testing.T) {
	aliases := []string{"groups:moderation", "groups:replies"}
	ceiling := []string{
		"groups:moderation", "groups:replies",
		"feature:blocked_words:enabled", "feature:blocked_words:words",
		"feature:blacklist:enabled", "feature:blacklist:members",
		"feature:exemptions:enabled", "feature:exemptions:members",
		"feature:keyword_replies:enabled", "feature:keyword_replies:admin_only", "feature:keyword_replies:rules",
	}
	for _, alias := range aliases {
		if !ceilingAllows(ceiling, alias) {
			t.Fatal("old ceiling lost a legacy alias after new fields were added", alias)
		}
	}
	actor := Actor{ID: NewID(), TenantID: NewID(), Role: "user"}
	q := &capabilityQueries{t: t, rows: []capabilityRow{
		{values: []any{aliases}},
		{values: []any{"owner", true}},
		{values: []any{[]string{}}},
		{values: []any{[]string{"moderation", "replies"}, 0}},
		{values: []any{ceiling}},
	}}
	got, err := EffectivePermissions(context.Background(), q, actor, BusinessPermissionCatalog())
	if err != nil || !slices.Equal(got, aliases) {
		t.Fatal("legacy-only member grants did not survive the old ceiling", got, err)
	}
	for _, test := range []struct {
		feature string
		fields  []string
	}{
		{feature: "blocked_words", fields: []string{"enabled", "words"}},
		{feature: "blacklist", fields: []string{"enabled", "members"}},
		{feature: "exemptions", fields: []string{"enabled", "members"}},
		{feature: "keyword_replies", fields: []string{"enabled", "admin_only", "rules"}},
	} {
		definition, ok := features.Find(test.feature)
		if !ok {
			t.Fatal("missing feature", test.feature)
		}
		for _, field := range test.fields {
			if !features.CanEdit(got, definition, field) {
				t.Fatal("legacy alias no longer covers its original field", test.feature, field)
			}
		}
	}
	for _, test := range []struct{ feature, field string }{
		{feature: "blocked_words", field: "rules"},
		{feature: "keyword_replies", field: "cooldown_seconds"},
	} {
		definition, ok := features.Find(test.feature)
		permission := features.Permission(test.feature, test.field)
		if !ok || ceilingAllows(ceiling, permission) || slices.Contains(got, permission) ||
			features.CanEdit(got, definition, test.field) {
			t.Fatal("legacy alias or ceiling granted a new field", permission)
		}
	}
}

func TestEffectivePermissionsFieldDenyClosesOnlyCoveringLegacyAlias(t *testing.T) {
	var grants []string
	for _, spec := range BusinessPermissionCatalog() {
		grants = append(grants, spec.Code)
	}
	for _, test := range []struct {
		feature string
		field   string
		alias   string
		closes  bool
	}{
		{feature: "blocked_words", field: "rules", alias: "groups:moderation"},
		{feature: "keyword_replies", field: "cooldown_seconds", alias: "groups:replies"},
		{feature: "blocked_words", field: "enabled", alias: "groups:moderation", closes: true},
		{feature: "blocked_words", field: "words", alias: "groups:moderation", closes: true},
		{feature: "blacklist", field: "enabled", alias: "groups:moderation", closes: true},
		{feature: "blacklist", field: "members", alias: "groups:moderation", closes: true},
		{feature: "exemptions", field: "enabled", alias: "groups:moderation", closes: true},
		{feature: "exemptions", field: "members", alias: "groups:moderation", closes: true},
		{feature: "keyword_replies", field: "enabled", alias: "groups:replies", closes: true},
		{feature: "keyword_replies", field: "admin_only", alias: "groups:replies", closes: true},
		{feature: "keyword_replies", field: "rules", alias: "groups:replies", closes: true},
	} {
		permission := features.Permission(test.feature, test.field)
		t.Run(permission, func(t *testing.T) {
			actor := Actor{ID: NewID(), TenantID: NewID(), Role: "user"}
			q := &capabilityQueries{t: t, rows: []capabilityRow{
				{values: []any{grants}},
				{values: []any{"owner", true}},
				{values: []any{[]string{permission + ":deny"}}},
				{values: []any{[]string{"moderation", "replies"}, 0}},
				{values: []any{grants}},
			}}
			got, err := EffectivePermissions(context.Background(), q, actor, BusinessPermissionCatalog())
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(got, permission) || slices.Contains(got, test.alias) == test.closes {
				t.Fatal("field deny changed the wrong legacy alias coverage", permission, got)
			}
			for _, alias := range []string{"groups:moderation", "groups:replies"} {
				if alias != test.alias && !slices.Contains(got, alias) {
					t.Fatal("field deny closed an unrelated legacy alias", permission, alias)
				}
			}
			definition, ok := features.Find(test.feature)
			if !ok || features.CanEdit(got, definition, test.field) {
				t.Fatal("legacy alias bypassed the denied field", permission)
			}
		})
	}
}

func TestEffectivePermissionsAreAnIntersection(t *testing.T) {
	ctx := context.Background()
	actor := Actor{ID: NewID(), TenantID: NewID(), Role: "user"}
	grants := []string{"members:manage", "groups:read", "assistant:use", "group_action:mute_member", "feature:keyword_replies:rules"}
	q := &capabilityQueries{t: t, rows: []capabilityRow{
		{values: []any{grants}},
		{values: []any{"manager", false}},
		{values: []any{[]string{"feature:keyword_replies:rules:deny"}}},
		{values: []any{[]string{"moderation", "replies", "management_assistant"}, 0}},
		{values: []any{[]string{"members:manage", "groups:read", "assistant:use", "feature:keyword_replies:rules"}}},
	}}
	got, err := EffectivePermissions(ctx, q, actor, BusinessPermissionCatalog())
	if err != nil || !slices.Equal(got, []string{"groups:read", "assistant:use"}) {
		t.Fatal("owner, override or ceiling intersection failed", got, err)
	}
	// A PLUS-like package cannot inherit assistant use even with explicit grants.
	q = &capabilityQueries{t: t, rows: []capabilityRow{
		{values: []any{grants}},
		{values: []any{"owner", true}},
		{values: []any{[]string{}}},
		{values: []any{[]string{"moderation", "replies"}, 0}},
		{values: []any{grants}},
	}}
	got, err = EffectivePermissions(ctx, q, actor, BusinessPermissionCatalog())
	if err != nil || slices.Contains(got, "assistant:use") {
		t.Fatal("package without assistant became entitled", got, err)
	}
	q = &capabilityQueries{t: t, rows: []capabilityRow{
		{values: []any{grants}}, {err: pgx.ErrNoRows},
	}}
	_, err = EffectivePermissions(ctx, q, actor, BusinessPermissionCatalog())
	capabilityError(t, err, "permission_denied")
}

func TestBusinessAdmissionIsScopedAndDoesNotUseTestSources(t *testing.T) {
	ctx, group, tenant := context.Background(), NewID(), NewID()
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, row := range []capabilityRow{
			{err: pgx.ErrNoRows}, {values: []any{"closed", false}}, {values: []any{"pilot", false}},
		} {
			q := &capabilityQueries{t: t, rows: []capabilityRow{row}}
			capabilityError(t, RequireBusinessAdmission(ctx, q, platform, group, "mute_member", "assistant"), "business_admission_closed")
		}
		ceiling := []string{"group_action:mute_member", "assistant:use", "assistant:action_propose", "assistant:action_approve"}
		q := &capabilityQueries{t: t, rows: []capabilityRow{
			{values: []any{"pilot", true}},
			{values: []any{true, platform}}, ordinaryControlledBindingRow(),
			{values: []any{tenant, []string{"moderation", "management_assistant"}, true, false, 0}},
			{values: []any{ceiling}}, {values: []any{ceiling}}, {values: []any{ceiling}}, {values: []any{ceiling}},
		}}
		if err := RequireBusinessAdmission(ctx, q, platform, group, "mute_member", "assistant"); err != nil {
			t.Fatal("explicit scoped pilot denied", platform, err)
		}
		q = &capabilityQueries{t: t, rows: []capabilityRow{{values: []any{"open", false}}}}
		if err := RequireBusinessAdmission(ctx, q, platform, group, "group_accept", "onboarding"); err != nil || q.calls != 1 {
			t.Fatal("onboarding pre-binding admission depends on an invented binding", err)
		}
	}
	q := &capabilityQueries{t: t}
	for _, source := range []string{"tests", "native_debug", "onboarding", ""} {
		capabilityError(t, RequireBusinessAdmission(ctx, q, "wsl", group, "mute_member", source), "business_capability_unavailable")
	}
	capabilityError(t, RequireBusinessAdmission(ctx, q, "wsl", group, "group_accept", "assistant"), "business_capability_unavailable")
}

func TestBusinessRuntimeOwnershipAndCancellationFailClosed(t *testing.T) {
	ctx := context.Background()
	q := &capabilityQueries{t: t, rows: []capabilityRow{{err: pgx.ErrNoRows}, {values: []any{false, "wsl"}}}}
	for range 2 {
		capabilityError(t, RequireGroupRuntime(ctx, q, NewID()), "group_runtime_disabled")
	}
	q = &capabilityQueries{t: t, rows: []capabilityRow{{err: pgx.ErrNoRows}}}
	capabilityError(t, RequireTenantOwner(ctx, q, Actor{ID: NewID(), TenantID: NewID(), Role: "user"}), "tenant_owner_required")
	capabilityError(t, RequireTenantOwner(ctx, q, Actor{ID: NewID(), Role: "superadmin"}), "tenant_owner_required")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := RequireGroupRuntime(cancelled, q, NewID()); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled read was replaced by cached admission", err)
	}
	q = &capabilityQueries{t: t, rows: []capabilityRow{{err: pgx.ErrNoRows}}}
	d, err := ScopedAssistantAction(ctx, q, "wsl", NewID(), "send_text")
	if err != nil || d.Production || d.BlockReason != "business_admission_closed" {
		t.Fatal("closed scope changed global readiness", d, err)
	}
}

func TestBusinessEvidenceReferencesAreBoundedIdentifiers(t *testing.T) {
	for _, refs := range [][]string{
		nil, {"docs/test-report.md#旺商聊闭环验收2026-11-05"}, {"docs/architecture/README.md"},
		{"acceptance:WSL-20261105-001", "acceptance:LAJIAOLIAO-20261105-001"},
	} {
		if !ValidBusinessEvidenceRefs(refs) {
			t.Fatal("valid reference rejected", refs)
		}
	}
	for _, refs := range [][]string{
		{""}, {"https://synthetic.example/report?token=secret"}, {"/tmp/report.md"},
		{"docs/../secrets.md"}, {"docs//report.md"}, {"docs/report.json"},
		{"docs/report.md#"}, {"docs/report.md#free text"}, {"sk-synthetic-secret"},
		{"docs/report.md", "docs/report.md"}, {"acceptance:body with spaces"},
		{"docs/" + strings.Repeat("a", 253) + ".md"}, make([]string, 17),
	} {
		if ValidBusinessEvidenceRefs(refs) {
			t.Fatal("unbounded or non-reference evidence accepted")
		}
	}
}
