-- Controlled onboarding proves only the operator's bounded demo use. It is
-- deliberately not a customer ownership proof or a native debug grant.
CREATE TABLE onboarding_test_scopes (
    id uuid PRIMARY KEY,
    actor_id uuid NOT NULL REFERENCES users(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    account_id uuid NOT NULL REFERENCES accounts(id),
    claimant_account_id uuid NOT NULL REFERENCES accounts(id),
    group_id uuid NOT NULL REFERENCES native_groups(id),
    actor_version bigint NOT NULL,
    account_epoch bigint NOT NULL,
    account_version bigint NOT NULL,
    claimant_epoch bigint NOT NULL,
    claimant_version bigint NOT NULL,
    claimant_native_id text NOT NULL CHECK(length(claimant_native_id) BETWEEN 1 AND 200),
    claimant_role text NOT NULL CHECK(claimant_role IN ('','owner','admin')),
    robot_role text NOT NULL CHECK(robot_role IN ('','owner','admin','member')),
    owner_identity_hash bytea CHECK(octet_length(owner_identity_hash)=32),
    create_key text NOT NULL CHECK(length(create_key) BETWEEN 8 AND 100),
    request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
    state text NOT NULL DEFAULT 'active' CHECK(state IN ('preparing','active','closed','expired','unknown')),
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    blocked_reason text NOT NULL DEFAULT '' CHECK(length(blocked_reason)<=100),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    closed_at timestamptz,
    CHECK(account_id<>claimant_account_id),
    CHECK((owner_identity_hash IS NULL)=(claimant_role='' AND robot_role='')),
    CHECK(expires_at>created_at AND expires_at<=created_at+interval '30 minutes 1 second'),
    UNIQUE(actor_id,create_key)
);
CREATE UNIQUE INDEX onboarding_test_scope_actor_live ON onboarding_test_scopes(actor_id,platform)
    WHERE state IN ('active','preparing');
CREATE UNIQUE INDEX onboarding_test_scope_group_live ON onboarding_test_scopes(group_id)
    WHERE state IN ('active','preparing');
CREATE INDEX onboarding_test_scope_expiry ON onboarding_test_scopes(expires_at)
    WHERE state IN ('active','preparing');
ALTER TABLE onboarding_test_scopes ENABLE ROW LEVEL SECURITY;
ALTER TABLE onboarding_test_scopes FORCE ROW LEVEL SECURITY;
CREATE POLICY platform_only ON onboarding_test_scopes
    USING(current_user='wsl_platform') WITH CHECK(current_user='wsl_platform');
GRANT SELECT,INSERT,UPDATE ON onboarding_test_scopes TO wsl_platform;

CREATE FUNCTION onboarding_test_scope_active(scope uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT EXISTS(SELECT 1 FROM onboarding_test_scopes s
        JOIN users u ON u.id=s.actor_id JOIN tenants t ON t.id=s.tenant_id
        JOIN accounts a ON a.id=s.account_id JOIN accounts c ON c.id=s.claimant_account_id
        JOIN native_groups g ON g.id=s.group_id
        WHERE s.id=scope AND s.state='active' AND s.expires_at>clock_timestamp()
        AND u.role='superadmin' AND NOT u.disabled AND u.access_version=s.actor_version
        AND t.space_kind='operator_demo' AND t.operator_owner_id=u.id
        AND a.platform=s.platform AND c.platform=s.platform AND g.platform=s.platform AND g.isolated_test
        AND a.purpose='management' AND a.workspace=s.platform
        AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL AND a.testing_enabled
        AND c.auth_state='authenticated' AND c.credential_sealed IS NOT NULL AND c.testing_enabled
        AND a.epoch=s.account_epoch AND a.version=s.account_version
        AND c.epoch=s.claimant_epoch AND c.version=s.claimant_version
        AND c.native_uid=s.claimant_native_id AND a.native_uid<>c.native_uid)
$$;

CREATE FUNCTION onboarding_test_scope_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'immutable controlled onboarding history' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' AND (
        (NEW.id,NEW.actor_id,NEW.tenant_id,NEW.platform,NEW.account_id,NEW.claimant_account_id,NEW.group_id,
         NEW.actor_version,NEW.account_epoch,NEW.account_version,NEW.claimant_epoch,NEW.claimant_version,
         NEW.claimant_native_id,NEW.claimant_role,NEW.robot_role,NEW.owner_identity_hash,NEW.create_key,
         NEW.request_hash,NEW.created_at,NEW.expires_at)
        IS DISTINCT FROM
        (OLD.id,OLD.actor_id,OLD.tenant_id,OLD.platform,OLD.account_id,OLD.claimant_account_id,OLD.group_id,
         OLD.actor_version,OLD.account_epoch,OLD.account_version,OLD.claimant_epoch,OLD.claimant_version,
         OLD.claimant_native_id,OLD.claimant_role,OLD.robot_role,OLD.owner_identity_hash,OLD.create_key,
         OLD.request_hash,OLD.created_at,OLD.expires_at)
        OR NEW.version<>OLD.version+1
        OR OLD.state IN ('closed','expired') AND NEW.state<>OLD.state) THEN
        RAISE EXCEPTION 'immutable controlled onboarding scope' USING ERRCODE='23514';
    END IF;
    IF TG_OP='INSERT' AND NOT EXISTS(
        SELECT 1 FROM users u JOIN tenants t ON t.id=NEW.tenant_id
        JOIN accounts a ON a.id=NEW.account_id JOIN accounts c ON c.id=NEW.claimant_account_id
        JOIN native_groups g ON g.id=NEW.group_id
        WHERE u.id=NEW.actor_id AND u.role='superadmin' AND NOT u.disabled
        AND u.access_version=NEW.actor_version AND workspace_is_demo(t.id,u.id)
        AND a.platform=NEW.platform AND c.platform=NEW.platform AND g.platform=NEW.platform AND g.isolated_test
        AND a.purpose='management' AND a.testing_enabled AND c.testing_enabled
        AND a.auth_state='authenticated' AND c.auth_state='authenticated'
        AND a.epoch=NEW.account_epoch AND a.version=NEW.account_version
        AND c.epoch=NEW.claimant_epoch AND c.version=NEW.claimant_version
        AND c.native_uid=NEW.claimant_native_id AND a.native_uid<>c.native_uid
        AND NEW.state='active' AND NEW.version=1) THEN
        RAISE EXCEPTION 'invalid controlled onboarding scope' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER onboarding_test_scope_immutable BEFORE INSERT OR UPDATE OR DELETE ON onboarding_test_scopes
    FOR EACH ROW EXECUTE FUNCTION onboarding_test_scope_immutable();

ALTER TABLE group_binding_challenges ADD COLUMN controlled_scope_id uuid REFERENCES onboarding_test_scopes(id);
ALTER TABLE group_binding_ownership ADD COLUMN proof_kind text NOT NULL DEFAULT 'authenticated_message'
    CHECK(proof_kind IN ('authenticated_message','controlled_test'));
ALTER TABLE group_binding_ownership ADD COLUMN controlled_scope_id uuid REFERENCES onboarding_test_scopes(id);
ALTER TABLE group_binding_ownership ADD CONSTRAINT controlled_ownership_proof
    CHECK((proof_kind='controlled_test')=(controlled_scope_id IS NOT NULL));
ALTER TABLE group_claims DROP CONSTRAINT group_claims_proof_method_check;
ALTER TABLE group_claims ADD CONSTRAINT group_claims_proof_method_check
    CHECK(proof_method IN ('manual','authenticated_message','controlled_test'));

CREATE FUNCTION controlled_onboarding_proof_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE scope uuid;
BEGIN
    IF TG_TABLE_NAME='group_binding_challenges' THEN
        IF TG_OP='UPDATE' AND NEW.controlled_scope_id IS DISTINCT FROM OLD.controlled_scope_id THEN
            RAISE EXCEPTION 'immutable challenge test scope' USING ERRCODE='23514';
        END IF;
        IF NEW.controlled_scope_id IS NULL THEN RETURN NEW; END IF;
        IF NOT EXISTS(SELECT 1 FROM onboarding_test_scopes s
            WHERE s.id=NEW.controlled_scope_id AND s.actor_id=NEW.applicant_id AND s.tenant_id=NEW.tenant_id
            AND s.platform=NEW.platform AND s.account_id=NEW.account_id
            AND s.account_epoch=NEW.account_epoch AND s.account_version=NEW.account_version
            AND NEW.applicant_mode='operator_demo' AND NEW.group_id=s.group_id) THEN
            RAISE EXCEPTION 'controlled challenge scope mismatch' USING ERRCODE='23514';
        END IF;
        IF TG_OP='INSERT' AND NOT onboarding_test_scope_active(NEW.controlled_scope_id) THEN
            RAISE EXCEPTION 'controlled challenge inactive' USING ERRCODE='23514';
        END IF;
    ELSIF TG_TABLE_NAME='group_binding_ownership' THEN
        SELECT controlled_scope_id INTO scope FROM group_binding_challenges WHERE id=NEW.proof_challenge_id;
        IF scope IS DISTINCT FROM NEW.controlled_scope_id
            OR (scope IS NOT NULL)<>(NEW.proof_kind='controlled_test') THEN
            RAISE EXCEPTION 'controlled ownership origin mismatch' USING ERRCODE='23514';
        END IF;
        IF scope IS NOT NULL AND NEW.state='active' AND NOT EXISTS(
            SELECT 1 FROM onboarding_test_scopes s WHERE s.id=scope AND s.group_id=NEW.group_id
            AND s.tenant_id=NEW.tenant_id AND s.platform=NEW.platform AND onboarding_test_scope_active(s.id)) THEN
            RAISE EXCEPTION 'controlled ownership inactive' USING ERRCODE='23514';
        END IF;
    ELSE
        SELECT controlled_scope_id INTO scope FROM group_binding_challenges WHERE id=NEW.onboarding_challenge_id;
        IF (scope IS NOT NULL)<>(NEW.proof_method='controlled_test') THEN
            RAISE EXCEPTION 'controlled claim origin mismatch' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER controlled_onboarding_challenge BEFORE INSERT OR UPDATE ON group_binding_challenges
    FOR EACH ROW EXECUTE FUNCTION controlled_onboarding_proof_scope();
CREATE TRIGGER controlled_onboarding_ownership BEFORE INSERT OR UPDATE ON group_binding_ownership
    FOR EACH ROW EXECUTE FUNCTION controlled_onboarding_proof_scope();
CREATE TRIGGER controlled_onboarding_claim BEFORE INSERT OR UPDATE ON group_claims
    FOR EACH ROW EXECUTE FUNCTION controlled_onboarding_proof_scope();
