CREATE TABLE ai_gateway_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
    enabled boolean NOT NULL DEFAULT false,
    platform_limit_micro bigint NOT NULL DEFAULT 0 CHECK(platform_limit_micro>=0),
    test_limit_micro bigint NOT NULL DEFAULT 0 CHECK(test_limit_micro>=0),
    version bigint NOT NULL DEFAULT 0
);
INSERT INTO ai_gateway_settings(singleton) VALUES(true);
CREATE TABLE ai_model_rates (
    id uuid PRIMARY KEY,
    alias text NOT NULL CHECK(length(alias) BETWEEN 1 AND 80),
    version bigint NOT NULL,
    provider_sealed bytea NOT NULL,
    input_rate bigint NOT NULL CHECK(input_rate BETWEEN 0 AND 1000000000000),
    output_rate bigint NOT NULL CHECK(output_rate BETWEEN 0 AND 1000000000000),
    cache_read_rate bigint NOT NULL CHECK(cache_read_rate BETWEEN 0 AND 1000000000000),
    cache_write_rate bigint NOT NULL CHECK(cache_write_rate BETWEEN 0 AND 1000000000000),
    verified boolean NOT NULL DEFAULT false,
    created_by uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(alias,version),
    CHECK(input_rate+output_rate>0)
);
CREATE TABLE ai_model_routes (
    alias text PRIMARY KEY,
    rate_id uuid NOT NULL REFERENCES ai_model_rates(id),
    enabled boolean NOT NULL DEFAULT false,
    version bigint NOT NULL DEFAULT 1
);
CREATE TABLE ai_calls (
    id uuid PRIMARY KEY,
    tenant_id uuid REFERENCES tenants(id),
    actor_id uuid NOT NULL REFERENCES users(id),
    group_id uuid REFERENCES native_groups(id),
    purpose text NOT NULL CHECK(purpose IN ('assistant','heatbot','test')),
    period_id uuid REFERENCES subscription_periods(id),
    rate_id uuid NOT NULL REFERENCES ai_model_rates(id),
    external_key text NOT NULL UNIQUE,
    request_hash bytea NOT NULL,
    reserved_micro bigint NOT NULL CHECK(reserved_micro>0),
    charged_micro bigint NOT NULL DEFAULT 0 CHECK(charged_micro>=0),
    status text NOT NULL DEFAULT 'reserved' CHECK(status IN ('reserved','submitted','settled','released','unknown')),
    usage jsonb,
    submitted_at timestamptz,
    finished_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ai_calls_budget ON ai_calls(created_at,tenant_id,status);
CREATE VIEW ai_call_costs WITH (security_invoker=true) AS
    SELECT c.*,c.charged_micro-coalesce((SELECT sum(e.amount_micro)::bigint FROM credit_entries e
        WHERE e.call_id=c.id AND e.kind='adjustment'),0) AS effective_charged_micro FROM ai_calls c;
GRANT SELECT ON ai_call_costs TO wsl_platform;
CREATE TABLE heat_gateway_grants (
    token_hash bytea PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    actor_id uuid NOT NULL REFERENCES users(id),
    group_id uuid NOT NULL REFERENCES native_groups(id),
    account_id uuid NOT NULL REFERENCES accounts(id),
    binding_version bigint NOT NULL,
    binding_epoch bigint NOT NULL,
    account_epoch bigint NOT NULL,
    persona_version bigint NOT NULL,
    model_alias text NOT NULL,
    route_version bigint NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at timestamptz
);
DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['ai_gateway_settings','ai_model_rates','ai_model_routes','ai_calls','heat_gateway_grants'] LOOP
        EXECUTE format('GRANT SELECT,INSERT,UPDATE ON %I TO wsl_platform',t);
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
        EXECUTE format('CREATE POLICY platform_only ON %I USING(current_user=''wsl_platform'')',t);
    END LOOP;
END $$;
REVOKE UPDATE,DELETE ON ai_model_rates FROM wsl_platform;
CREATE TRIGGER immutable_model_rate BEFORE UPDATE OR DELETE ON ai_model_rates FOR EACH ROW EXECUTE FUNCTION immutable_billing_record();
