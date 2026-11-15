-- Operator test claims prove the logged-in pool account's native authority.
-- They are never customer ownership proofs and cannot admit background work.
CREATE TABLE operator_demo_group_claims (
    id uuid PRIMARY KEY,
    actor_id uuid NOT NULL REFERENCES users(id),
    actor_version bigint NOT NULL,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    group_id uuid NOT NULL REFERENCES native_groups(id),
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    account_id uuid NOT NULL REFERENCES accounts(id),
    account_epoch bigint NOT NULL,
    account_version bigint NOT NULL,
    binding_epoch bigint NOT NULL,
    binding_version bigint NOT NULL,
    create_key text NOT NULL CHECK(length(create_key) BETWEEN 8 AND 100),
    state text NOT NULL DEFAULT 'checking' CHECK(state IN ('checking','active','failed','cancelled')),
    robot_role text NOT NULL DEFAULT '' CHECK(robot_role IN ('','owner','admin')),
    owner_identity_hash bytea CHECK(octet_length(owner_identity_hash)=32),
    blocked_reason text NOT NULL DEFAULT '' CHECK(length(blocked_reason)<=100),
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '10 minutes',
    CHECK(expires_at>created_at AND expires_at<=created_at+interval '10 minutes 1 second'),
    CHECK(state<>'active' OR robot_role<>'' AND owner_identity_hash IS NOT NULL),
    UNIQUE(actor_id,create_key)
);
CREATE INDEX operator_demo_claim_history
    ON operator_demo_group_claims(actor_id,platform,group_id,created_at DESC);
CREATE UNIQUE INDEX operator_demo_claim_live
    ON operator_demo_group_claims(actor_id,group_id) WHERE state IN ('checking','active');

CREATE FUNCTION operator_demo_claim_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' AND NOT EXISTS(
        SELECT 1 FROM users u JOIN group_bindings b ON b.group_id=NEW.group_id
        JOIN native_groups g ON g.id=b.group_id JOIN accounts a ON a.id=b.primary_account_id
        WHERE u.id=NEW.actor_id AND u.role='superadmin' AND NOT u.disabled
        AND u.access_version=NEW.actor_version AND b.active AND b.tenant_id=NEW.tenant_id
        AND b.version=NEW.binding_version AND b.epoch=NEW.binding_epoch
        AND g.platform=NEW.platform AND a.id=NEW.account_id AND a.platform=NEW.platform
        AND a.workspace=NEW.platform AND a.purpose='management'
        AND a.epoch=NEW.account_epoch AND a.version=NEW.account_version
        AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
    ) THEN RAISE EXCEPTION 'invalid operator demo claim scope' USING ERRCODE='23514'; END IF;
    IF TG_OP='UPDATE' AND (
        (NEW.id,NEW.actor_id,NEW.actor_version,NEW.tenant_id,NEW.group_id,NEW.platform,
         NEW.account_id,NEW.account_epoch,NEW.account_version,NEW.binding_epoch,NEW.binding_version,
         NEW.create_key,NEW.created_at,NEW.expires_at)
        IS DISTINCT FROM
        (OLD.id,OLD.actor_id,OLD.actor_version,OLD.tenant_id,OLD.group_id,OLD.platform,
         OLD.account_id,OLD.account_epoch,OLD.account_version,OLD.binding_epoch,OLD.binding_version,
         OLD.create_key,OLD.created_at,OLD.expires_at)
        OR OLD.state<>'checking' AND NEW.state NOT IN (OLD.state,'cancelled')
        OR OLD.state<>'checking' AND (NEW.robot_role,NEW.owner_identity_hash)
            IS DISTINCT FROM (OLD.robot_role,OLD.owner_identity_hash)
    ) THEN RAISE EXCEPTION 'immutable operator demo claim' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER operator_demo_claim_scope BEFORE INSERT OR UPDATE ON operator_demo_group_claims
    FOR EACH ROW EXECUTE FUNCTION operator_demo_claim_scope();
GRANT SELECT,INSERT,UPDATE ON operator_demo_group_claims TO wsl_platform;
ALTER TABLE operator_demo_group_claims ENABLE ROW LEVEL SECURITY;
ALTER TABLE operator_demo_group_claims FORCE ROW LEVEL SECURITY;
CREATE POLICY platform_only ON operator_demo_group_claims
    USING(current_user='wsl_platform') WITH CHECK(current_user='wsl_platform');
