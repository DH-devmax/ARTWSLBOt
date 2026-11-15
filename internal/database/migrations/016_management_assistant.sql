ALTER TABLE plans ADD COLUMN monthly_assistant_tokens bigint NOT NULL DEFAULT 0
    CHECK (monthly_assistant_tokens BETWEEN 0 AND 1000000000000);

CREATE TABLE assistant_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    enabled boolean NOT NULL DEFAULT false,
    tenant_access boolean NOT NULL DEFAULT false,
    approvals_enabled boolean NOT NULL DEFAULT false,
    monthly_tokens bigint NOT NULL DEFAULT 0 CHECK (monthly_tokens BETWEEN 0 AND 1000000000000),
    sealed bytea,
    version bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO assistant_settings(singleton) VALUES(true);

CREATE TABLE assistant_sessions (
    id uuid PRIMARY KEY,
    actor_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id uuid REFERENCES tenants(id),
    domain text NOT NULL DEFAULT 'wsl' CHECK (domain='wsl'),
    scope_kind text NOT NULL CHECK (scope_kind IN ('overview','account','group')),
    account_id uuid REFERENCES accounts(id) ON DELETE CASCADE,
    group_id uuid REFERENCES native_groups(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL DEFAULT now()+interval '7 days',
    CHECK ((scope_kind='overview' AND account_id IS NULL AND group_id IS NULL)
        OR (scope_kind='account' AND account_id IS NOT NULL AND group_id IS NULL)
        OR (scope_kind='group' AND account_id IS NOT NULL AND group_id IS NOT NULL))
);
CREATE TABLE assistant_turns (
    id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES assistant_sessions(id) ON DELETE CASCADE,
    actor_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id uuid REFERENCES tenants(id),
    domain text NOT NULL DEFAULT 'wsl' CHECK (domain='wsl'),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 8 AND 100),
    input_hash bytea NOT NULL,
    authorization_hash text NOT NULL,
    input_sealed bytea,
    status text NOT NULL CHECK (status IN ('queued','running','succeeded','failed','cancelled','expired','interrupted')),
    result_code text NOT NULL DEFAULT '',
    cancel_requested boolean NOT NULL DEFAULT false,
    deadline timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    UNIQUE (actor_id,idempotency_key)
);
CREATE UNIQUE INDEX assistant_one_active_turn ON assistant_turns(actor_id)
    WHERE status IN ('queued','running');
CREATE TABLE assistant_events (
    turn_id uuid NOT NULL REFERENCES assistant_turns(id) ON DELETE CASCADE,
    seq bigint NOT NULL CHECK (seq>0),
    actor_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id uuid REFERENCES tenants(id),
    domain text NOT NULL DEFAULT 'wsl' CHECK (domain='wsl'),
    kind text NOT NULL CHECK (kind IN ('turn_started','text_delta','tool_started','tool_result',
        'plan_created','usage','turn_finished','error')),
    payload_sealed bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(turn_id,seq)
);
CREATE TABLE assistant_usage (
    id uuid PRIMARY KEY,
    turn_id uuid NOT NULL,
    actor_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id uuid REFERENCES tenants(id),
    domain text NOT NULL DEFAULT 'wsl' CHECK (domain='wsl'),
    month date NOT NULL,
    reserved bigint NOT NULL CHECK (reserved>0),
    charged bigint NOT NULL DEFAULT 0 CHECK (charged>=0),
    state text NOT NULL CHECK (state IN ('reserved','actual','estimated','unknown')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(turn_id,id)
);
CREATE INDEX assistant_usage_month ON assistant_usage(month,tenant_id);
CREATE FUNCTION assistant_month_usage() RETURNS TABLE(used bigint, held bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,public AS $$
    SELECT coalesce(sum(CASE WHEN state='actual' THEN charged ELSE greatest(reserved,charged) END),0)::bigint,
           coalesce(sum(reserved) FILTER(WHERE state IN ('reserved','unknown','estimated')),0)::bigint
    FROM public.assistant_usage
    WHERE month=date_trunc('month',now() AT TIME ZONE 'Asia/Shanghai')::date
      AND tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid
$$;
REVOKE ALL ON FUNCTION assistant_month_usage() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION assistant_month_usage() TO wsl_tenant;
CREATE TABLE assistant_plans (
    id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES assistant_sessions(id) ON DELETE CASCADE,
    actor_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    domain text NOT NULL DEFAULT 'wsl' CHECK (domain='wsl'),
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    group_id uuid NOT NULL REFERENCES native_groups(id) ON DELETE CASCADE,
    base_version bigint NOT NULL,
    account_epoch bigint NOT NULL,
    binding_epoch bigint NOT NULL,
    digest text NOT NULL,
    payload_sealed bytea,
    status text NOT NULL DEFAULT 'pending_approval'
        CHECK (status IN ('pending_approval','committed','failed','expired','cancelled','unknown')),
    version bigint NOT NULL DEFAULT 1,
    execution_id uuid UNIQUE,
    committed_version bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL DEFAULT now()+interval '10 minutes',
    committed_at timestamptz
);
CREATE INDEX assistant_history ON assistant_sessions(actor_id,created_at DESC);
CREATE INDEX assistant_turn_history ON assistant_turns(session_id,created_at DESC);
CREATE TABLE assistant_plan_executions (
    id uuid PRIMARY KEY,
    plan_id uuid UNIQUE NOT NULL,
    actor_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    domain text NOT NULL DEFAULT 'wsl' CHECK (domain='wsl'),
    group_id uuid NOT NULL,
    config_version bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['assistant_sessions','assistant_turns','assistant_events','assistant_usage','assistant_plans','assistant_plan_executions']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('CREATE POLICY assistant_actor_scope ON %I USING
            (current_user=''wsl_platform'' OR
             (actor_id=nullif(current_setting(''app.actor_id'',true),'''')::uuid
              AND tenant_id=nullif(current_setting(''app.tenant_id'',true),'''')::uuid
              AND domain=current_setting(''app.domain'',true)))
            WITH CHECK (current_user=''wsl_platform'')', t);
        EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I TO wsl_platform',t);
        EXECUTE format('GRANT SELECT ON %I TO wsl_tenant',t);
    END LOOP;
END $$;
GRANT SELECT,UPDATE ON assistant_settings TO wsl_platform;
REVOKE ALL ON assistant_settings FROM wsl_tenant;
