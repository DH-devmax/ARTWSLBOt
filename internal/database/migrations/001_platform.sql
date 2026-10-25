CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE tenants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid REFERENCES tenants(id),
    username text UNIQUE NOT NULL CHECK (length(username) BETWEEN 3 AND 80),
    password_hash text NOT NULL,
    role text NOT NULL CHECK (role IN ('superadmin','user')),
    disabled boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((role = 'superadmin' AND tenant_id IS NULL) OR (role = 'user' AND tenant_id IS NOT NULL))
);
CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY,
    csrf_hash bytea NOT NULL,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE plans (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    group_limit integer NOT NULL CHECK (group_limit BETWEEN 0 AND 100),
    ai_group_limit integer NOT NULL CHECK (ai_group_limit BETWEEN 0 AND 30 AND ai_group_limit <= group_limit),
    monthly_ai_tokens bigint NOT NULL DEFAULT 0 CHECK (monthly_ai_tokens >= 0),
    modules text[] NOT NULL DEFAULT '{}',
    version bigint NOT NULL DEFAULT 1
);
CREATE TABLE subscriptions (
    tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
    plan_id uuid NOT NULL REFERENCES plans(id),
    expires_at timestamptz NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1
);
CREATE TABLE accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    native_uid text UNIQUE,
    connection_enabled boolean NOT NULL DEFAULT false,
    testing_enabled boolean NOT NULL DEFAULT false,
    runtime_state text NOT NULL DEFAULT 'disconnected',
    epoch bigint NOT NULL DEFAULT 1,
    version bigint NOT NULL DEFAULT 1,
    credential_sealed bytea,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE native_groups (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    native_id text UNIQUE NOT NULL CHECK (length(native_id) BETWEEN 1 AND 200),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    isolated_test boolean NOT NULL DEFAULT false
);
CREATE TABLE account_groups (
    account_id uuid NOT NULL REFERENCES accounts(id),
    group_id uuid NOT NULL REFERENCES native_groups(id),
    role text NOT NULL CHECK (role IN ('unknown','member','admin','owner','left')),
    observed_at timestamptz NOT NULL,
    observation_source text NOT NULL CHECK (observation_source IN ('native','replay')),
    PRIMARY KEY (account_id, group_id)
);
CREATE TABLE personas (
    account_id uuid PRIMARY KEY REFERENCES accounts(id),
    nickname text NOT NULL,
    personality text NOT NULL CHECK (length(personality) <= 12000),
    reply_style text NOT NULL DEFAULT '' CHECK (length(reply_style) <= 4000),
    version bigint NOT NULL DEFAULT 1
);
CREATE TABLE group_bindings (
    group_id uuid PRIMARY KEY REFERENCES native_groups(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    primary_account_id uuid NOT NULL REFERENCES accounts(id),
    ai_account_id uuid REFERENCES accounts(id),
    management_enabled boolean NOT NULL DEFAULT false,
    ai_enabled boolean NOT NULL DEFAULT false,
    active boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1,
    epoch bigint NOT NULL DEFAULT 1,
    config jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, group_id)
);
CREATE INDEX bindings_tenant ON group_bindings(tenant_id);
CREATE TABLE claim_codes (
    code_hash bytea PRIMARY KEY,
    group_id uuid NOT NULL REFERENCES native_groups(id),
    account_id uuid NOT NULL REFERENCES accounts(id),
    account_epoch bigint NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz
);
CREATE UNIQUE INDEX one_live_code ON claim_codes(group_id) WHERE consumed_at IS NULL;
CREATE TABLE group_claims (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    group_id uuid NOT NULL REFERENCES native_groups(id),
    account_id uuid NOT NULL REFERENCES accounts(id),
    applicant_id uuid NOT NULL REFERENCES users(id),
    native_identity text NOT NULL CHECK (length(native_identity) BETWEEN 1 AND 200),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','approved','rejected','expired')),
    review_note text NOT NULL DEFAULT '',
    reviewed_by uuid REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    reviewed_at timestamptz
);
CREATE TABLE test_windows (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id uuid NOT NULL REFERENCES users(id),
    group_id uuid NOT NULL REFERENCES native_groups(id),
    account_ids uuid[] NOT NULL CHECK (cardinality(account_ids) BETWEEN 1 AND 10),
    target_ids text[] NOT NULL,
    actions text[] NOT NULL CHECK (cardinality(actions) BETWEEN 1 AND 6),
    account_epochs jsonb NOT NULL,
    expires_at timestamptz NOT NULL,
    budget integer NOT NULL CHECK (budget BETWEEN 1 AND 10),
    used integer NOT NULL DEFAULT 0 CHECK (used >= 0 AND used <= budget),
    cancelled boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at <= created_at + interval '300 seconds')
);
CREATE TABLE operations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid REFERENCES tenants(id),
    actor_id uuid REFERENCES users(id),
    group_id uuid NOT NULL REFERENCES native_groups(id),
    account_id uuid NOT NULL REFERENCES accounts(id),
    test_window_id uuid REFERENCES test_windows(id),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 8 AND 100),
    request_hash bytea NOT NULL,
    kind text NOT NULL CHECK (kind IN ('send_text','recall','mute','unmute')),
    target_id text NOT NULL DEFAULT '',
    payload_sealed bytea,
    account_epoch bigint NOT NULL,
    binding_epoch bigint,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN
        ('queued','running','succeeded','failed','cancelled','expired','unknown')),
    expires_at timestamptz NOT NULL,
    result_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (actor_id, idempotency_key)
);
CREATE INDEX operations_queue ON operations(status, created_at) WHERE status = 'queued';
CREATE INDEX operations_tenant ON operations(tenant_id, created_at DESC);
CREATE TABLE inbox (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts(id),
    group_id uuid NOT NULL REFERENCES native_groups(id),
    native_message_id text NOT NULL,
    sender_id text NOT NULL,
    received_at timestamptz NOT NULL,
    persisted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    payload_sealed bytea,
    state text NOT NULL CHECK (state IN ('pending','unresolved','processed','quarantined')),
    UNIQUE (account_id, group_id, native_message_id)
);
CREATE INDEX inbox_pending ON inbox(id) WHERE state IN ('pending','unresolved');
CREATE INDEX inbox_retention ON inbox(received_at) WHERE payload_sealed IS NOT NULL;
CREATE TABLE audit_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id uuid REFERENCES tenants(id),
    actor_id uuid REFERENCES users(id),
    action text NOT NULL,
    resource_id text NOT NULL DEFAULT '',
    detail jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_tenant ON audit_events(tenant_id, id DESC);
CREATE TABLE ledger_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    group_id uuid NOT NULL,
    member_id text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('reward','adjustment','paid')),
    amount_cents bigint NOT NULL CHECK (amount_cents BETWEEN -100000000000 AND 100000000000),
    external_key text NOT NULL,
    evidence jsonb NOT NULL,
    actor_id uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, group_id) REFERENCES group_bindings(tenant_id, group_id),
    UNIQUE (group_id, external_key)
);
CREATE TABLE activities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    group_id uuid NOT NULL,
    name text NOT NULL,
    state text NOT NULL DEFAULT 'open' CHECK (state IN ('open','closed','drawn')),
    closes_at timestamptz NOT NULL,
    winners integer NOT NULL CHECK (winners BETWEEN 1 AND 100),
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, group_id) REFERENCES group_bindings(tenant_id, group_id)
);
CREATE TABLE activity_members (
    activity_id uuid NOT NULL REFERENCES activities(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    member_id text NOT NULL,
    winner boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (activity_id, member_id)
);

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['subscriptions','group_bindings','group_claims','operations',
        'audit_events','ledger_entries','activities','activity_members']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY tenant_scope ON %I USING
             (current_user = ''wsl_platform'' OR
              tenant_id = nullif(current_setting(''app.tenant_id'', true), '''')::uuid)
             WITH CHECK (current_user = ''wsl_platform'' OR
              tenant_id = nullif(current_setting(''app.tenant_id'', true), '''')::uuid)', t);
    END LOOP;
END $$;

GRANT USAGE ON SCHEMA public TO wsl_platform, wsl_tenant;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO wsl_platform;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO wsl_platform;
GRANT SELECT ON group_bindings, subscriptions, group_claims, operations, audit_events,
    ledger_entries, activities, activity_members, plans TO wsl_tenant;
