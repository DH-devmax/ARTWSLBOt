CREATE TABLE heat_chat_admission (
    platform text PRIMARY KEY CHECK(platform IN ('wsl','lajiaoliao')),
    enabled boolean NOT NULL DEFAULT false,
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO heat_chat_admission(platform) VALUES('wsl'),('lajiaoliao');

CREATE TABLE heat_chat_consumers (
    group_id uuid PRIMARY KEY REFERENCES native_groups(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    binding_epoch bigint NOT NULL,
    starts_at timestamptz NOT NULL DEFAULT now(),
    last_scan_at timestamptz NOT NULL DEFAULT now(),
    last_send_at timestamptz
);
CREATE TABLE heat_chat_receipts (
    group_id uuid NOT NULL REFERENCES native_groups(id),
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    event_id bigint NOT NULL,
    decision text NOT NULL CHECK(decision IN ('ignored','sampled_out','cooldown','queued')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(group_id,account_id,event_id)
);
CREATE INDEX heat_chat_receipts_retention ON heat_chat_receipts(created_at);

CREATE TABLE heat_chat_jobs (
    id uuid PRIMARY KEY,
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    actor_id uuid NOT NULL REFERENCES users(id),
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    group_id uuid NOT NULL REFERENCES native_groups(id),
    event_id bigint NOT NULL,
    directed boolean NOT NULL,
    scope_digest bytea NOT NULL CHECK(octet_length(scope_digest)=32),
    scope_sealed bytea NOT NULL,
    response_sealed bytea,
    status text NOT NULL DEFAULT 'queued' CHECK(status IN
        ('queued','generating','generated','sending','sent','failed','unknown','superseded','cancelled','expired')),
    result_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(group_id,account_id,event_id)
);
CREATE INDEX heat_chat_jobs_pending ON heat_chat_jobs(status,created_at)
    WHERE status IN ('queued','generating','generated','sending');
CREATE UNIQUE INDEX heat_chat_jobs_one_pending ON heat_chat_jobs(group_id) WHERE status='queued';
CREATE UNIQUE INDEX heat_chat_jobs_one_active ON heat_chat_jobs(group_id)
    WHERE status IN ('generating','generated','sending');

CREATE TABLE heat_chat_send_attempts (
    job_id uuid PRIMARY KEY REFERENCES heat_chat_jobs(id) ON DELETE CASCADE,
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    client_id uuid NOT NULL UNIQUE,
    receiver_owner text NOT NULL,
    receiver_revision bigint NOT NULL,
    nonce numeric(20,0) UNIQUE,
    aad numeric(20,0),
    message_at timestamptz NOT NULL DEFAULT now(),
    route_digest bytea,
    route_deployment bigint,
    CHECK((platform='wsl' AND nonce>0 AND aad>0) OR
        (platform='lajiaoliao' AND nonce IS NULL AND aad IS NULL))
);
CREATE TABLE heat_chat_account_cooldowns (
    account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    next_send_at timestamptz NOT NULL
);
ALTER TABLE heat_gateway_grants ADD COLUMN chat_job_id uuid REFERENCES heat_chat_jobs(id) ON DELETE CASCADE;
ALTER TABLE heat_gateway_grants ADD COLUMN generation_deadline timestamptz;
CREATE UNIQUE INDEX heat_gateway_chat_job ON heat_gateway_grants(chat_job_id) WHERE chat_job_id IS NOT NULL;

CREATE FUNCTION immutable_heat_chat_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='heat_chat_jobs' THEN
        IF (NEW.id,NEW.platform,NEW.tenant_id,NEW.actor_id,NEW.account_id,NEW.group_id,
            NEW.event_id,NEW.directed,NEW.scope_digest,NEW.scope_sealed,NEW.created_at,NEW.expires_at)
            IS DISTINCT FROM
            (OLD.id,OLD.platform,OLD.tenant_id,OLD.actor_id,OLD.account_id,OLD.group_id,
            OLD.event_id,OLD.directed,OLD.scope_digest,OLD.scope_sealed,OLD.created_at,OLD.expires_at) THEN
            RAISE EXCEPTION 'immutable heat chat scope';
        END IF;
    ELSE
        IF (NEW.job_id,NEW.platform,NEW.client_id,NEW.receiver_owner,NEW.receiver_revision,
            NEW.nonce,NEW.aad,NEW.message_at) IS DISTINCT FROM
            (OLD.job_id,OLD.platform,OLD.client_id,OLD.receiver_owner,OLD.receiver_revision,
            OLD.nonce,OLD.aad,OLD.message_at) OR
            (OLD.route_digest IS NOT NULL AND
                (NEW.route_digest,NEW.route_deployment) IS DISTINCT FROM (OLD.route_digest,OLD.route_deployment)) THEN
            RAISE EXCEPTION 'immutable heat chat send attempt';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER immutable_heat_chat_job BEFORE UPDATE ON heat_chat_jobs
    FOR EACH ROW EXECUTE FUNCTION immutable_heat_chat_identity();
CREATE TRIGGER immutable_heat_chat_attempt BEFORE UPDATE ON heat_chat_send_attempts
    FOR EACH ROW EXECUTE FUNCTION immutable_heat_chat_identity();

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['heat_chat_admission','heat_chat_consumers','heat_chat_receipts',
        'heat_chat_jobs','heat_chat_send_attempts','heat_chat_account_cooldowns'] LOOP
        EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I TO wsl_platform',t);
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
        EXECUTE format('CREATE POLICY platform_only ON %I USING(current_user=''wsl_platform'')',t);
    END LOOP;
END $$;
