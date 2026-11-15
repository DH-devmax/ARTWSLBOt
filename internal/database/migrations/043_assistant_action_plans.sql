CREATE TABLE assistant_action_plans (
    id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES assistant_sessions(id) ON DELETE CASCADE,
    actor_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    domain text NOT NULL CHECK(domain IN ('wsl','lajiaoliao')),
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    group_id uuid NOT NULL REFERENCES native_groups(id) ON DELETE CASCADE,
    action text NOT NULL CHECK(action IN ('publish_notice','member_nickname','mute_member',
        'unmute_member','mute_group','unmute_group','kick_member','send_text','recall_message','group_info')),
    digest text NOT NULL CHECK(length(digest)=64),
    authorization_hash text NOT NULL,
    payload_sealed bytea NOT NULL,
    account_epoch bigint NOT NULL,
    binding_epoch bigint NOT NULL,
    base_version bigint NOT NULL,
    status text NOT NULL CHECK(status IN ('pending_approval','blocked','approved','submitting',
        'succeeded','failed','unknown','cancelled','expired')),
    result_code text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1,
    idempotency_key text,
    operation_id uuid,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '10 minutes',
    UNIQUE(actor_id,idempotency_key)
);
CREATE INDEX assistant_action_history ON assistant_action_plans(session_id,created_at DESC);
ALTER TABLE assistant_action_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE assistant_action_plans FORCE ROW LEVEL SECURITY;
CREATE POLICY assistant_actor_scope ON assistant_action_plans USING(
    current_user='wsl_platform' OR
    (actor_id=nullif(current_setting('app.actor_id',true),'')::uuid
    AND tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid
    AND domain=current_setting('app.domain',true))
) WITH CHECK(current_user='wsl_platform');
GRANT SELECT,INSERT,UPDATE,DELETE ON assistant_action_plans TO wsl_platform;
GRANT SELECT ON assistant_action_plans TO wsl_tenant;
CREATE TRIGGER assistant_scope_guard BEFORE INSERT OR UPDATE ON assistant_action_plans
    FOR EACH ROW EXECUTE FUNCTION assistant_consistent_scope();

CREATE FUNCTION assistant_action_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.session_id,NEW.actor_id,NEW.tenant_id,NEW.domain,NEW.account_id,NEW.group_id,
        NEW.action,NEW.digest,NEW.authorization_hash,NEW.payload_sealed,NEW.account_epoch,
        NEW.binding_epoch,NEW.base_version,NEW.expires_at)
        IS DISTINCT FROM
        (OLD.session_id,OLD.actor_id,OLD.tenant_id,OLD.domain,OLD.account_id,OLD.group_id,
        OLD.action,OLD.digest,OLD.authorization_hash,OLD.payload_sealed,OLD.account_epoch,
        OLD.binding_epoch,OLD.base_version,OLD.expires_at) THEN
        RAISE EXCEPTION 'immutable action plan' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER assistant_action_payload_guard BEFORE UPDATE ON assistant_action_plans
    FOR EACH ROW EXECUTE FUNCTION assistant_action_immutable();

CREATE TABLE assistant_target_choices (
    session_id uuid PRIMARY KEY REFERENCES assistant_sessions(id) ON DELETE CASCADE,
    actor_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id uuid REFERENCES tenants(id),
    domain text NOT NULL CHECK(domain IN ('wsl','lajiaoliao')),
    token_digest text NOT NULL,
    expires_at timestamptz NOT NULL
);
ALTER TABLE assistant_target_choices ENABLE ROW LEVEL SECURITY;
ALTER TABLE assistant_target_choices FORCE ROW LEVEL SECURITY;
CREATE POLICY assistant_actor_scope ON assistant_target_choices USING(
    current_user='wsl_platform' OR
    (actor_id=nullif(current_setting('app.actor_id',true),'')::uuid
    AND tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid
    AND domain=current_setting('app.domain',true))
) WITH CHECK(current_user='wsl_platform');
GRANT SELECT,INSERT,UPDATE,DELETE ON assistant_target_choices TO wsl_platform;
GRANT SELECT ON assistant_target_choices TO wsl_tenant;
