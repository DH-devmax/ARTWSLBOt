package control

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/features"
)

type Failure struct {
	Status int
	Code   string
}

func (e *Failure) Error() string           { return e.Code }
func Denied(status int, code string) error { return &Failure{status, code} }

type FeatureChanges map[string]map[string]json.RawMessage

func ContentField(id, field string) bool {
	if field == "enabled" && slices.Contains([]string{"blocked_words", "blacklist", "exemptions", "keyword_replies"}, id) {
		return true
	}
	return id == "blocked_words" && field == "words" ||
		(id == "blacklist" || id == "exemptions") && field == "members" ||
		id == "keyword_replies" && (field == "rules" || field == "admin_only")
}

func ValidateFeatures(previous features.Config, changes FeatureChanges, actor Actor, grants, modules []string, contentOnly bool) (features.Config, map[string][]string, error) {
	// Marshal/unmarshal makes the snapshot independent of caller-owned slices/maps.
	blob, err := json.Marshal(previous)
	if err != nil {
		return previous, nil, err
	}
	var config features.Config
	if err = json.Unmarshal(blob, &config); err != nil {
		return previous, nil, err
	}
	if len(changes) == 0 || len(changes) > len(features.Catalog()) {
		return previous, nil, Denied(400, "invalid_feature_settings")
	}
	changed := map[string][]string{}
	for id, fields := range changes {
		definition, ok := features.Find(id)
		if !ok || len(definition.Fields) == 0 || len(fields) == 0 || len(fields) > len(definition.Fields) {
			return previous, nil, Denied(400, "invalid_feature_settings")
		}
		if !slices.Contains(modules, definition.Module) {
			return previous, nil, Denied(403, "module_not_in_plan")
		}
		for field, value := range fields {
			if contentOnly && !ContentField(id, field) {
				return previous, nil, Denied(403, "assistant_field_forbidden")
			}
			if actor.Role != "superadmin" && !features.CanEdit(grants, definition, field) {
				return previous, nil, Denied(403, "permission_denied")
			}
			if err = features.Apply(&config, id, field, value); err != nil {
				return previous, nil, Denied(400, "invalid_feature_settings")
			}
			changed[id] = append(changed[id], field)
		}
		slices.Sort(changed[id])
	}
	return config, changed, nil
}

type FeatureCommit struct {
	Version int64 `json:"version"`
	Applied bool  `json:"applied"`
}

// SaveFeaturesTx is the single field-config transaction used by WebUI and approval.
// The caller owns commit so approval evidence and this write can be atomic.
func SaveFeaturesTx(ctx context.Context, tx pgx.Tx, actor Actor, catalog []PermissionSpec,
	group string, version int64, changes FeatureChanges, contentOnly bool) (FeatureCommit, error) {
	var result FeatureCommit
	if err := RequireGroup(ctx, tx, actor, group); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return result, err
	}
	var tenant, workspace string
	var raw []byte
	var current int64
	var active, detached bool
	err := tx.QueryRow(ctx, `SELECT b.tenant_id::text,b.config,b.version,b.active,b.primary_account_id IS NULL,
        coalesce(a.workspace,'') FROM group_bindings b LEFT JOIN accounts a ON a.id=b.primary_account_id
        WHERE b.group_id=$1 FOR UPDATE OF b`, group).Scan(&tenant, &raw, &current, &active, &detached, &workspace)
	if err == pgx.ErrNoRows || err == nil && actor.Role != "superadmin" && tenant != actor.TenantID {
		return result, Denied(404, "group_not_found")
	}
	if err != nil {
		return result, err
	}
	if !active {
		return result, Denied(409, "group_authorization_inactive")
	}
	if detached {
		return result, Denied(409, "group_robot_removed")
	}
	if actor.Role != "superadmin" {
		if err = requireFeatureCeilings(ctx, tx, tenant, changes); err != nil {
			return result, err
		}
	}
	if workspace != "wsl" && workspace != "lajiaoliao" {
		return result, Denied(403, "workspace_route_forbidden")
	}
	for id := range changes {
		if !slices.ContainsFunc(features.CatalogForPlatform(workspace), func(d features.Definition) bool { return d.ID == id }) {
			return result, Denied(400, "feature_not_available_on_platform")
		}
	}
	if current != version {
		return result, Denied(409, "version_conflict")
	}
	entitlement, err := ResolveHumanEntitlement(ctx, tx, actor, catalog, workspace, group)
	if err != nil {
		return result, err
	}
	if !entitlement.Active {
		return result, Denied(403, "active_subscription_required")
	}
	grants, err := LiveActorPermissions(ctx, tx, actor, catalog)
	if err != nil {
		return result, err
	}
	var previous features.Config
	if err = json.Unmarshal(raw, &previous); err != nil {
		return result, err
	}
	config, changed, err := ValidateFeatures(previous, changes, actor, grants, entitlement.Modules, contentOnly)
	if err != nil {
		return result, err
	}
	return CommitGroupConfigTx(ctx, tx, actor, tenant, group, version, config, nil, nil, "group_features_updated", map[string]any{"fields": changed})
}

// CommitGroupConfigTx is shared by full-config, field-config and assistant approval.
// Authorization/row locks precede it; CAS, epoch invalidation and audit are atomic.
func CommitGroupConfigTx(ctx context.Context, tx pgx.Tx, actor Actor, tenant, group string, version int64,
	config features.Config, management, ai *bool, action string, detail map[string]any) (FeatureCommit, error) {
	var result FeatureCommit
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return result, err
	}
	var platform string
	if err := tx.QueryRow(ctx, `SELECT g.platform FROM native_groups g
		JOIN group_bindings b ON b.group_id=g.id WHERE b.group_id=$1 AND b.tenant_id=$2 AND b.active`,
		group, tenant).Scan(&platform); err != nil {
		if err == pgx.ErrNoRows {
			return result, Denied(404, "group_not_found")
		}
		return result, err
	}
	if err := RequireHumanGroupOwnership(ctx, tx, actor, platform, group); err != nil {
		return result, err
	}
	blob, err := json.Marshal(config)
	if err != nil {
		return result, err
	}
	err = tx.QueryRow(ctx, `UPDATE group_bindings SET config=$2,version=version+1,epoch=epoch+1,
        management_enabled=coalesce($4,management_enabled),ai_enabled=coalesce($5,ai_enabled)
        WHERE group_id=$1 AND version=$3 RETURNING version`, group, blob, version, management, ai).Scan(&result.Version)
	if err == pgx.ErrNoRows {
		return result, Denied(409, "version_conflict")
	}
	if err != nil {
		return result, err
	}
	if actor.Role == "superadmin" {
		if err = continueOperatorDemoClaimAfterConfig(ctx, tx, actor, tenant, platform, group, version, result.Version); err != nil {
			return result, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE operations SET status='cancelled',result_code='group_features_changed',
        updated_at=now() WHERE group_id=$1 AND status='queued'`, group); err != nil {
		return result, err
	}
	detail["version"], detail["applied"] = result.Version, false
	audit, _ := json.Marshal(detail)
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
        VALUES($1,$2,$3,$4,$5)`, tenant, actor.ID, action, group, audit)
	return result, err
}
