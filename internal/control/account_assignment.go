package control

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

type AccountCapacity struct {
	Bound int  `json:"bound"`
	Limit int  `json:"limit"`
	Over  bool `json:"over_capacity"`
}

func ReadAccountCapacity(ctx context.Context, q Queryer, account string) (AccountCapacity, error) {
	out := AccountCapacity{Limit: MaxGroupsPerAccount}
	err := q.QueryRow(ctx, `SELECT count(*) FROM group_bindings WHERE active
        AND (primary_account_id=$1 OR ai_account_id=$1)`, account).Scan(&out.Bound)
	out.Over = out.Bound > out.Limit
	return out, err
}

// Call under the shared admission transaction lock. Retaining an existing
// binding does not consume another slot, even if legacy usage exceeds its cap.
func CheckAccountCapacity(ctx context.Context, tx pgx.Tx, account, group string) error {
	var used int
	var retained bool
	err := tx.QueryRow(ctx, `SELECT count(*),coalesce(bool_or(group_id=$2),false)
        FROM group_bindings WHERE active AND (primary_account_id=$1 OR ai_account_id=$1)`,
		account, group).Scan(&used, &retained)
	if err != nil {
		return err
	}
	if !retained && used >= MaxGroupsPerAccount {
		return Denied(409, "account_group_limit_reached")
	}
	return nil
}

type AssignmentCandidate struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Epoch       int64      `json:"epoch"`
	Bound       int        `json:"bound_groups"`
	Eligible    bool       `json:"eligible"`
	Reasons     []string   `json:"reasons"`
	Role        string     `json:"native_role"`
	RoleAt      *time.Time `json:"role_observed_at"`
	Queue       int        `json:"receive_queue"`
	WaitMS      float64    `json:"http_wait_ms"`
	ReceiveRate float64    `json:"receive_per_second"`
	DurableP95  *float64   `json:"durable_p95_ms"`
}

type AssignmentPreview struct {
	GroupID     string                `json:"group_id"`
	Workspace   string                `json:"workspace"`
	Source      string                `json:"source_account_id"`
	SourceEpoch int64                 `json:"source_epoch"`
	Version     int64                 `json:"version"`
	Epoch       int64                 `json:"binding_epoch"`
	Tenant      string                `json:"-"`
	Active      bool                  `json:"active"`
	Candidates  []AssignmentCandidate `json:"candidates"`
	Recommended string                `json:"recommended_account_id"`
	SampledAt   time.Time             `json:"sampled_at"`
}

func AssignmentOptions(ctx context.Context, tx pgx.Tx, group, workspace string) (AssignmentPreview, error) {
	out := AssignmentPreview{GroupID: group, Workspace: workspace, SampledAt: time.Now().UTC(), Candidates: []AssignmentCandidate{}}
	if workspace != "wsl" && workspace != "maibot" && workspace != "lajiaoliao" {
		return out, Denied(400, "invalid_workspace")
	}
	err := tx.QueryRow(ctx, `SELECT tenant_id::text,active,version,epoch,
        coalesce((CASE WHEN $2='maibot' THEN ai_account_id ELSE primary_account_id END)::text,'')
        FROM group_bindings WHERE group_id=$1`, group, workspace).Scan(&out.Tenant, &out.Active, &out.Version, &out.Epoch, &out.Source)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "group_not_found")
	}
	if err != nil {
		return out, err
	}
	if !out.Active {
		return out, Denied(409, "group_authorization_inactive")
	}
	if out.Source != "" {
		if err = tx.QueryRow(ctx, `SELECT epoch FROM accounts WHERE id=$1`, out.Source).Scan(&out.SourceEpoch); err != nil {
			return out, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT a.id::text,a.name,a.epoch,a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL,
        (SELECT count(*) FROM group_bindings b WHERE active AND (b.primary_account_id=a.id OR b.ai_account_id=a.id)),
        coalesce(ag.role,'unknown'),ag.observed_at,coalesce(ag.observation_source,''),
        coalesce(r.enabled AND r.lease_until>now() AND r.observed_at>now()-interval '5 seconds',false),
        coalesce(r.snapshot,'{}'),s.cooldown_until
        FROM accounts a LEFT JOIN account_groups ag ON ag.account_id=a.id AND ag.group_id=$1
        LEFT JOIN account_receivers r ON r.account_id=a.id
        LEFT JOIN account_read_state s ON s.account_id=a.id AND s.account_epoch=a.epoch
        JOIN native_groups ng ON ng.id=$1 AND ng.platform=a.platform
        WHERE a.purpose=CASE WHEN $2='maibot' THEN 'chat' ELSE 'management' END
        AND ($2='maibot' OR a.platform=$2) ORDER BY a.id LIMIT 10`, group, workspace)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var c AssignmentCandidate
		var authenticated, observed bool
		var source string
		var raw []byte
		var cooldown *time.Time
		err = rows.Scan(&c.ID, &c.Name, &c.Epoch, &authenticated, &c.Bound, &c.Role, &c.RoleAt, &source, &observed, &raw, &cooldown)
		if err != nil {
			return out, err
		}
		c.Reasons = []string{}
		if !authenticated {
			c.Reasons = append(c.Reasons, "native_login_required")
		}
		if c.ID != out.Source && c.Bound >= MaxGroupsPerAccount {
			c.Reasons = append(c.Reasons, "account_group_limit_reached")
		}
		roleOK := c.Role == "owner" || c.Role == "admin" || workspace == "maibot" && c.Role == "member"
		if !roleOK || source != "native" || c.RoleAt == nil || out.SampledAt.Sub(*c.RoleAt) > 60*time.Second || c.RoleAt.After(out.SampledAt.Add(time.Second)) {
			c.Reasons = append(c.Reasons, "fresh_native_role_required")
		}
		var metrics struct {
			State       string     `json:"state"`
			SyncedAt    *time.Time `json:"synced_at"`
			HeartbeatAt *time.Time `json:"heartbeat_at"`
			Queue       int        `json:"queue_depth"`
			Rate        float64    `json:"per_second"`
			Durable     *float64   `json:"durable_p95_ms"`
		}
		_ = json.Unmarshal(raw, &metrics)
		if !observed || metrics.State != "connected" || metrics.SyncedAt == nil || metrics.HeartbeatAt == nil || out.SampledAt.Sub(*metrics.HeartbeatAt) > 40*time.Second {
			c.Reasons = append(c.Reasons, "receiver_unobserved")
		}
		if cooldown != nil && cooldown.After(out.SampledAt) {
			c.Reasons = append(c.Reasons, "native_rate_limited")
		}
		c.Queue, c.ReceiveRate, c.DurableP95 = metrics.Queue, metrics.Rate, metrics.Durable
		c.Eligible = len(c.Reasons) == 0
		out.Candidates = append(out.Candidates, c)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	SortAssignmentCandidates(out.Candidates)
	for _, c := range out.Candidates {
		if c.Eligible {
			out.Recommended = c.ID
			break
		}
	}
	return out, nil
}

func SortAssignmentCandidates(candidates []AssignmentCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.Eligible != b.Eligible {
			return a.Eligible
		}
		if a.Bound != b.Bound {
			return a.Bound < b.Bound
		}
		if a.WaitMS != b.WaitMS {
			return a.WaitMS < b.WaitMS
		}
		if a.Queue != b.Queue {
			return a.Queue < b.Queue
		}
		if (a.DurableP95 == nil) != (b.DurableP95 == nil) {
			return a.DurableP95 != nil
		}
		if a.DurableP95 != nil && b.DurableP95 != nil && *a.DurableP95 != *b.DurableP95 {
			return *a.DurableP95 < *b.DurableP95
		}
		if a.ReceiveRate != b.ReceiveRate {
			return a.ReceiveRate < b.ReceiveRate
		}
		return a.ID < b.ID
	})
}

func CheckAssignmentEntitlement(ctx context.Context, tx pgx.Tx, tenant, group, workspace string) error {
	if workspace != "wsl" && workspace != "lajiaoliao" && workspace != "maibot" {
		return Denied(400, "invalid_workspace")
	}
	var demo bool
	if err := tx.QueryRow(ctx, `SELECT workspace_is_demo(b.tenant_id) FROM group_bindings b
		JOIN native_groups g ON g.id=b.group_id
		WHERE b.group_id=$1 AND b.tenant_id=$2 AND b.active
		AND ($3='maibot' OR g.platform=$3) FOR SHARE OF b`, group, tenant, workspace).Scan(&demo); err != nil {
		if err == pgx.ErrNoRows {
			return Denied(404, "group_not_found")
		}
		return err
	}
	if demo {
		service, err := WorkspaceServiceEntitlement(ctx, tx, tenant)
		if err != nil {
			return err
		}
		if !service.Demo {
			return Denied(409, "workspace_service_changed")
		}
		var total int
		err = tx.QueryRow(ctx, `SELECT count(*) FROM group_bindings WHERE active AND group_id<>$1
			AND ($2<>'maibot' OR ai_account_id IS NOT NULL OR ai_requested OR ai_enabled)`,
			group, workspace).Scan(&total)
		if err != nil {
			return err
		}
		if total >= MaxAuthorizedGroups {
			return Denied(409, "group_quota_exceeded")
		}
		return nil
	}
	var limit, used, total int
	var modules []string
	err := tx.QueryRow(ctx, `SELECT CASE WHEN $2='maibot' THEN p.ai_group_limit ELSE p.group_limit END,p.modules
        FROM subscriptions s JOIN plans p ON p.id=s.plan_id
        WHERE s.tenant_id=$1 AND s.enabled AND s.expires_at>clock_timestamp()
        AND EXISTS(SELECT 1 FROM native_groups g WHERE g.id=$3 AND g.platform=ANY(p.allowed_platforms))
        FOR SHARE OF s`, tenant, workspace, group).Scan(&limit, &modules)
	if err == pgx.ErrNoRows {
		return Denied(409, "active_subscription_required")
	}
	if err != nil {
		return err
	}
	if workspace == "maibot" {
		allowed := false
		for _, m := range modules {
			if m == "ai" {
				allowed = true
			}
		}
		if !allowed {
			return Denied(403, "module_not_in_plan")
		}
	}
	err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE tenant_id=$1),count(*) FROM group_bindings
        WHERE active AND group_id<>$2 AND ($3<>'maibot' OR ai_account_id IS NOT NULL OR ai_requested OR ai_enabled)`,
		tenant, group, workspace).Scan(&used, &total)
	if err != nil {
		return err
	}
	if used >= limit || total >= MaxAuthorizedGroups {
		return Denied(409, "group_quota_exceeded")
	}
	return nil
}
