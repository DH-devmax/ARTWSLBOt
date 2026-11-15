-- Control-owned consumers run after durable projection, never in ingress/ACK.
CREATE TABLE business_rule_consumers (
    group_id uuid PRIMARY KEY REFERENCES native_groups(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    binding_epoch bigint NOT NULL CHECK(binding_epoch>0),
    runtime_version bigint NOT NULL CHECK(runtime_version>0),
    starts_at timestamptz NOT NULL,
    source_watermark bigint NOT NULL DEFAULT 0 CHECK(source_watermark>=0),
    late_watermark bigint NOT NULL DEFAULT 0 CHECK(late_watermark>=0),
    last_scan_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    last_recovery_at timestamptz,
    last_reply_at timestamptz
);

CREATE TABLE business_rule_receipts (
    group_id uuid NOT NULL REFERENCES native_groups(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    event_id bigint NOT NULL,
    decision text NOT NULL CHECK(decision IN
        ('ignored','challenge','role_unknown','matched','queued','cooldown','limited','blocked','expired')),
    rule text NOT NULL DEFAULT '' CHECK(rule IN ('','blocked_words','blacklist','keyword_replies')),
    result_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(group_id,account_id,event_id)
);
CREATE INDEX business_rule_receipts_retention ON business_rule_receipts(created_at);

CREATE TABLE business_rule_jobs (
    id uuid PRIMARY KEY,
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    actor_id uuid NOT NULL REFERENCES users(id),
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    group_id uuid NOT NULL REFERENCES native_groups(id),
    event_id bigint NOT NULL,
    rule text NOT NULL CHECK(rule IN ('blocked_words','blacklist','keyword_replies')),
    action text NOT NULL CHECK(action IN ('recall_message','mute_member','send_text')),
    priority integer NOT NULL CHECK(priority IN (0,1)),
    scope_snapshot bytea NOT NULL CHECK(octet_length(scope_snapshot) BETWEEN 1 AND 32768),
    scope_sealed bytea,
    payload_sealed bytea,
    status text NOT NULL DEFAULT 'queued' CHECK(status IN
        ('queued','executing','submitting','succeeded','failed','unknown','superseded','cancelled','expired')),
    result_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK((rule='blocked_words' AND action='recall_message' AND priority=0)
        OR (rule='blacklist' AND action='mute_member' AND priority=0)
        OR (rule='keyword_replies' AND action='send_text' AND priority=1)),
    CHECK(expires_at<=created_at+interval '120 seconds'),
    UNIQUE(group_id,account_id,event_id)
);
CREATE INDEX business_rule_jobs_pending ON business_rule_jobs(status,priority,created_at)
    WHERE status IN ('queued','executing','submitting');
CREATE INDEX business_rule_jobs_history ON business_rule_jobs(group_id,created_at DESC);
CREATE INDEX business_rule_jobs_unknown_account ON business_rule_jobs(account_id) WHERE status='unknown';
CREATE UNIQUE INDEX business_rule_jobs_one_pending ON business_rule_jobs(group_id) WHERE status='queued';
CREATE UNIQUE INDEX business_rule_jobs_one_active ON business_rule_jobs(group_id)
    WHERE status IN ('executing','submitting');
CREATE UNIQUE INDEX business_rule_jobs_account_active ON business_rule_jobs(account_id)
    WHERE status IN ('executing','submitting');

CREATE TABLE business_rule_attempts (
    job_id uuid PRIMARY KEY REFERENCES business_rule_jobs(id) ON DELETE CASCADE,
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    action text NOT NULL CHECK(action IN ('recall_message','mute_member','send_text')),
    client_id uuid NOT NULL UNIQUE,
    receiver_owner text NOT NULL CHECK(length(receiver_owner)>0),
    receiver_revision bigint NOT NULL CHECK(receiver_revision>0),
    nonce numeric(20,0) UNIQUE,
    aad numeric(20,0),
    message_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    route_digest bytea CHECK(route_digest IS NULL OR octet_length(route_digest)=32),
    route_deployment bigint,
    submitted_at timestamptz,
    CHECK((platform='wsl' AND action='send_text' AND nonce>0 AND aad>0)
        OR ((platform<>'wsl' OR action<>'send_text') AND nonce IS NULL AND aad IS NULL)),
    CHECK((route_digest IS NULL)=(route_deployment IS NULL))
);

CREATE TABLE business_rule_account_cooldowns (
    account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    next_write_at timestamptz NOT NULL
);
-- Shared-account admission also checks recently completed manual operations.
CREATE INDEX native_setting_account_recent ON native_setting_operations(account_id,updated_at DESC);
CREATE TABLE business_rule_member_cooldowns (
    group_id uuid NOT NULL REFERENCES native_groups(id),
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    member_id text NOT NULL CHECK(length(member_id) BETWEEN 1 AND 200),
    until_at timestamptz NOT NULL,
    PRIMARY KEY(group_id,account_id,member_id)
);

CREATE FUNCTION business_rule_consistent_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM group_bindings b
        JOIN native_groups g ON g.id=b.group_id
        JOIN accounts a ON a.id=b.primary_account_id
        WHERE b.group_id=NEW.group_id AND b.tenant_id=NEW.tenant_id
        AND a.id=NEW.account_id AND a.purpose='management'
        AND a.platform=NEW.platform AND g.platform=NEW.platform) THEN
        RAISE EXCEPTION 'business rule scope mismatch' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME='business_rule_jobs' THEN
        IF NOT EXISTS(SELECT 1 FROM users u JOIN tenants t ON t.owner_id=u.id
            WHERE u.id=NEW.actor_id AND t.id=NEW.tenant_id AND u.tenant_id=t.id
            AND u.tenant_role='owner' AND NOT u.disabled) THEN
            RAISE EXCEPTION 'business rule actor mismatch' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER business_rule_consumer_scope BEFORE INSERT OR UPDATE ON business_rule_consumers
    FOR EACH ROW EXECUTE FUNCTION business_rule_consistent_scope();
CREATE TRIGGER business_rule_receipt_scope BEFORE INSERT ON business_rule_receipts
    FOR EACH ROW EXECUTE FUNCTION business_rule_consistent_scope();
CREATE TRIGGER business_rule_job_scope BEFORE INSERT ON business_rule_jobs
    FOR EACH ROW EXECUTE FUNCTION business_rule_consistent_scope();

CREATE FUNCTION immutable_business_rule_job() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.id,NEW.platform,NEW.tenant_id,NEW.actor_id,NEW.account_id,NEW.group_id,
        NEW.event_id,NEW.rule,NEW.action,NEW.priority,NEW.scope_snapshot,NEW.created_at,NEW.expires_at)
        IS DISTINCT FROM
        (OLD.id,OLD.platform,OLD.tenant_id,OLD.actor_id,OLD.account_id,OLD.group_id,
        OLD.event_id,OLD.rule,OLD.action,OLD.priority,OLD.scope_snapshot,OLD.created_at,OLD.expires_at)
        OR (NEW.scope_sealed IS DISTINCT FROM OLD.scope_sealed AND NEW.scope_sealed IS NOT NULL)
        OR (NEW.payload_sealed IS DISTINCT FROM OLD.payload_sealed AND NEW.payload_sealed IS NOT NULL) THEN
        RAISE EXCEPTION 'immutable business rule job' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER immutable_business_rule_job BEFORE UPDATE ON business_rule_jobs
    FOR EACH ROW EXECUTE FUNCTION immutable_business_rule_job();

CREATE FUNCTION immutable_business_rule_attempt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF NOT EXISTS(SELECT 1 FROM business_rule_jobs j WHERE j.id=NEW.job_id
            AND j.platform=NEW.platform AND j.action=NEW.action AND j.status='executing') THEN
            RAISE EXCEPTION 'business attempt scope mismatch' USING ERRCODE='23514';
        END IF;
    ELSIF (NEW.job_id,NEW.platform,NEW.action,NEW.client_id,NEW.receiver_owner,
        NEW.receiver_revision,NEW.nonce,NEW.aad,NEW.message_at) IS DISTINCT FROM
        (OLD.job_id,OLD.platform,OLD.action,OLD.client_id,OLD.receiver_owner,
        OLD.receiver_revision,OLD.nonce,OLD.aad,OLD.message_at)
        OR (OLD.submitted_at IS NOT NULL AND NEW.submitted_at IS DISTINCT FROM OLD.submitted_at)
        OR (OLD.route_digest IS NOT NULL AND
            (NEW.route_digest,NEW.route_deployment) IS DISTINCT FROM (OLD.route_digest,OLD.route_deployment)) THEN
        RAISE EXCEPTION 'immutable business rule attempt' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER immutable_business_rule_attempt BEFORE INSERT OR UPDATE ON business_rule_attempts
    FOR EACH ROW EXECUTE FUNCTION immutable_business_rule_attempt();

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['business_rule_consumers','business_rule_receipts','business_rule_jobs',
        'business_rule_attempts','business_rule_account_cooldowns','business_rule_member_cooldowns'] LOOP
        EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I TO wsl_platform',t);
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
        EXECUTE format('CREATE POLICY platform_only ON %I USING(current_user=''wsl_platform'')',t);
    END LOOP;
END $$;
