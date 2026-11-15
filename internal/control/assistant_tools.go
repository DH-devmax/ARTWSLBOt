package control

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"wangshangliao/internal/assistant"
	"wangshangliao/internal/features"
)

func (s *AssistantService) queryTool(ctx context.Context, ref SessionRef, session AssistantSession, call assistant.ToolCall) (assistant.ToolResult, error) {
	out := assistant.ToolResult{Type: call.Function.Name, Scope: session.Scope, SampledAt: time.Now().UTC(),
		Verification: "configuration_only_not_runtime_verified"}
	name := call.Function.Name
	if !slices.Contains([]string{"list_authorized_bots", "get_bot_status", "list_authorized_groups", "get_group_config", "list_group_operations"}, name) {
		return out, Denied(400, "assistant_invalid_tool")
	}
	var args struct {
		AccountID string `json:"account_id"`
		GroupID   string `json:"group_id"`
	}
	visibleControlled := strings.ReplaceAll(controlledBindingVisibleSQL, "$4", "$5")
	argumentJSON := strings.TrimSpace(call.Function.Arguments)
	// Bound read tools have no model-supplied parameters. Some providers omit
	// their argument object; this does not permit ignoring nonempty input.
	if session.Scope.Kind == "group" && argumentJSON == "" {
		argumentJSON = "{}"
	}
	if !strings.HasPrefix(argumentJSON, "{") {
		return out, Denied(400, "assistant_invalid_tool_arguments")
	}
	if err := strictJSON(argumentJSON, &args); err != nil {
		return out, Denied(400, "assistant_invalid_tool_arguments")
	}
	if session.Scope.Kind == "group" {
		if !ValidID(session.Scope.AccountID) || !ValidID(session.Scope.GroupID) {
			return out, Denied(400, "assistant_invalid_scope")
		}
		if name == "get_bot_status" && args.AccountID == "" {
			args.AccountID = session.Scope.AccountID
		}
		if (name == "get_group_config" || name == "list_group_operations") && args.GroupID == "" {
			args.GroupID = session.Scope.GroupID
		}
	}
	if (name == "list_authorized_bots" || name == "list_authorized_groups") && (args.AccountID != "" || args.GroupID != "") ||
		name == "get_bot_status" && (args.AccountID == "" || args.GroupID != "") ||
		(name == "get_group_config" || name == "list_group_operations") && (args.GroupID == "" || args.AccountID != "") {
		return out, Denied(400, "assistant_invalid_tool_selector")
	}
	if args.AccountID != "" && !ValidID(args.AccountID) || args.GroupID != "" && !ValidID(args.GroupID) {
		return out, Denied(400, "assistant_invalid_tool_selector")
	}
	if session.Scope.AccountID != "" && args.AccountID != "" && session.Scope.AccountID != args.AccountID ||
		session.Scope.GroupID != "" && args.GroupID != "" && session.Scope.GroupID != args.GroupID {
		return out, Denied(403, "assistant_scope_mismatch")
	}
	account := session.Scope.AccountID
	if args.AccountID != "" {
		account = args.AccountID
	}
	group := session.Scope.GroupID
	if args.GroupID != "" {
		group = args.GroupID
	}
	if name == "get_bot_status" && account == "" || (name == "get_group_config" || name == "list_group_operations") && group == "" {
		return out, Denied(400, "assistant_invalid_tool")
	}
	a, err := s.authorize(ctx, s.DB.Platform, ref, session.Scope, "assistant:use", true, false)
	if err != nil {
		return out, err
	}
	grants, err := EffectivePermissions(ctx, s.DB.Platform, a, s.Catalog)
	if err != nil {
		return out, err
	}
	policy, err := assistantExposure(ctx, s.DB.Platform, session.Scope.Platform)
	if err != nil {
		return out, err
	}
	if name != "get_group_config" {
		d, ok := features.FindAssistantCapability(session.Scope.Platform, "workspace.query."+name)
		if !ok {
			return out, Denied(403, "assistant_invalid_tool")
		}
		if err = s.requireAssistantCapability(ctx, s.DB.Platform, a, session.Scope, policy, d); err != nil {
			return out, err
		}
	}
	if !slices.Contains(grants, "groups:read") {
		return out, Denied(403, "permission_denied")
	}
	// Configuration bodies use RLS. Directory metadata additionally checks the
	// control-private ownership anchor with explicit tenant/member predicates.
	if name == "list_group_operations" {
		if !slices.Contains(grants, "operations:read") {
			return out, Denied(403, "permission_denied")
		}
		out.Data, err = SafeBusinessOperations(ctx, s.DB.Platform, a, ref.assistantPlatform(), group, 20)
	} else {
		err = s.read(ctx, a, ref.assistantPlatform(), func(q Queryer) error {
			if group != "" {
				var targetAccount string
				err := q.QueryRow(ctx, `SELECT b.primary_account_id::text FROM group_bindings b
                JOIN accounts a ON a.id=b.primary_account_id WHERE b.group_id=$1 AND b.active AND a.workspace=$3
                AND ($2='' OR a.id=nullif($2,'')::uuid)`, group, account, ref.assistantPlatform()).Scan(&targetAccount)
				if err != nil {
					return Denied(404, "assistant_scope_not_found")
				}
				account = targetAccount
			}
			var raw []byte
			switch name {
			case "list_authorized_bots", "get_bot_status":
				err := s.DB.Platform.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
                SELECT a.id,coalesce(nullif(a.native_nickname,''),a.name) AS name,a.runtime_state,a.connection_enabled,
                    count(b.group_id)::int AS authorized_groups FROM accounts a
                JOIN group_bindings b ON b.primary_account_id=a.id AND b.active
                JOIN tenants t ON t.id=b.tenant_id
                LEFT JOIN group_binding_ownership o ON o.group_id=b.group_id AND o.tenant_id=b.tenant_id AND o.platform=a.platform
                WHERE a.workspace=$3 AND ($1='' OR a.id=nullif($1,'')::uuid)
                  AND `+visibleControlled+`
                  AND ($2='' OR b.group_id=nullif($2,'')::uuid)
                  AND (($4 AND (t.space_kind='tenant_business' OR workspace_is_demo(t.id,$5::uuid)))
                    OR (NOT $4 AND o.state='active' AND b.tenant_id=nullif($6,'')::uuid
                      AND member_group_visible(b.tenant_id,b.group_id,$5::uuid)))
                GROUP BY a.id ORDER BY a.id LIMIT 10) x`, account, group, ref.assistantPlatform(), a.Role == "superadmin", a.ID, a.TenantID).Scan(&raw)
				if err != nil {
					return err
				}
				if name == "get_bot_status" && string(raw) == "[]" {
					return Denied(404, "assistant_scope_not_found")
				}
				out.Data = json.RawMessage(raw)
			case "list_authorized_groups":
				err := s.DB.Platform.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
                SELECT b.group_id AS id,g.name,b.primary_account_id AS account_id,b.management_enabled,b.version,
                    b.epoch FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
                JOIN accounts a ON a.id=b.primary_account_id
                JOIN tenants t ON t.id=b.tenant_id
                LEFT JOIN group_binding_ownership o ON o.group_id=b.group_id AND o.tenant_id=b.tenant_id AND o.platform=g.platform
                WHERE b.active AND a.workspace=$3
                    AND `+visibleControlled+`
                    AND ($1='' OR a.id=nullif($1,'')::uuid) AND ($2='' OR b.group_id=nullif($2,'')::uuid)
                    AND (($4 AND (t.space_kind='tenant_business' OR workspace_is_demo(t.id,$5::uuid)))
                      OR (NOT $4 AND o.state='active' AND b.tenant_id=nullif($6,'')::uuid
                        AND member_group_visible(b.tenant_id,b.group_id,$5::uuid)))
                ORDER BY b.group_id LIMIT 100) x`, account, group, ref.assistantPlatform(), a.Role == "superadmin", a.ID, a.TenantID).Scan(&raw)
				if err != nil {
					return err
				}
				out.Data = json.RawMessage(raw)
			case "get_group_config":
				var config features.Config
				var version int64
				entitlement, err := ResolveHumanEntitlement(ctx, s.DB.Platform, a, s.Catalog, ref.assistantPlatform(), group)
				if err != nil {
					return err
				}
				err = q.QueryRow(ctx, `SELECT b.config,b.version FROM group_bindings b
                WHERE b.group_id=$1 AND b.active`, group).Scan(&raw, &version)
				if err != nil {
					return err
				}
				if err = json.Unmarshal(raw, &config); err != nil {
					return err
				}
				values := map[string]any{}
				for _, definition := range features.CatalogForPlatform(ref.assistantPlatform()) {
					d, ok := features.FindAssistantCapability(session.Scope.Platform, definition.ID+".query")
					if !ok {
						continue
					}
					if e := s.requireAssistantCapability(ctx, s.DB.Platform, a, session.Scope, policy, d); e != nil {
						if _, ok := e.(*Failure); ok {
							continue
						}
						return e
					}
					if !slices.Contains(entitlement.Modules, definition.Module) {
						continue
					}
					fields := map[string]any{}
					for field, value := range features.Values(config, definition.ID) {
						if ContentField(definition.ID, field) && (a.Role == "superadmin" || features.CanRead(grants, definition, field)) {
							fields[field] = value
						}
					}
					if len(fields) > 0 {
						values[definition.ID] = fields
					}
				}
				groupAccess, err := ResolveHumanGroupAccess(ctx, s.DB.Platform, a, ref.assistantPlatform(), group)
				if err != nil {
					return err
				}
				out.Data = map[string]any{"group_id": group, "account_id": account, "version": version, "values": values, "group_access": groupAccess}
			}
			return nil
		})
	}
	if err != nil {
		return out, err
	}
	raw, _ := json.Marshal(out.Data)
	if len(raw) > assistant.MaxResultBytes-1024 {
		// Never hand an incomplete rule set to a proposal generator.
		out.Truncated = true
		out.Data = map[string]any{"reason": "result_too_large", "group_id": group, "account_id": account}
	}
	_, err = s.authorize(ctx, s.DB.Platform, ref, session.Scope, "assistant:use", true, false)
	if err == nil && group != "" {
		_, err = s.authorize(ctx, s.DB.Platform, ref, assistant.Scope{Platform: session.Scope.Platform, Kind: "group", AccountID: account, GroupID: group}, "assistant:use", true, false)
	}
	return out, err
}
