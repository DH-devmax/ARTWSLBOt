-- Additive metadata only. Existing credentials, budgets, rates and consumers
-- retain their settings. A snapshot cannot authorize a paid model request.
CREATE TABLE ai_upstream_snapshots (
    rate_id uuid PRIMARY KEY REFERENCES ai_model_rates(id),
    snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
    checked_by uuid NOT NULL REFERENCES users(id),
    checked_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
GRANT SELECT,INSERT,UPDATE ON ai_upstream_snapshots TO wsl_platform;
REVOKE ALL ON ai_upstream_snapshots FROM wsl_tenant;
ALTER TABLE ai_upstream_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_upstream_snapshots FORCE ROW LEVEL SECURITY;
CREATE POLICY platform_only ON ai_upstream_snapshots
    USING(current_user='wsl_platform');
