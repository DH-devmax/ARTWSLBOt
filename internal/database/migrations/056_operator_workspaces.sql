-- Spaces describe resource ownership. A platform operator remains a superadmin
-- with no customer tenant membership; no commercial subscription is fabricated.
ALTER TABLE tenants ADD COLUMN space_kind text NOT NULL DEFAULT 'tenant_business'
    CHECK(space_kind IN ('tenant_business','operator_demo'));
ALTER TABLE tenants ADD COLUMN operator_owner_id uuid REFERENCES users(id);
ALTER TABLE tenants ADD CONSTRAINT operator_workspace_shape CHECK(
    (space_kind='operator_demo' AND operator_owner_id IS NOT NULL AND owner_id=operator_owner_id)
    OR (space_kind='tenant_business' AND operator_owner_id IS NULL));
CREATE UNIQUE INDEX operator_workspace_owner ON tenants(operator_owner_id)
    WHERE space_kind='operator_demo';

CREATE FUNCTION workspace_is_demo(space uuid, actor uuid DEFAULT NULL) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS(SELECT 1 FROM tenants t JOIN users u ON u.id=t.operator_owner_id
        WHERE t.id=space AND t.space_kind='operator_demo' AND t.owner_id=u.id
        AND u.role='superadmin' AND u.tenant_id IS NULL AND NOT u.disabled
        AND (actor IS NULL OR u.id=actor))
$$;
REVOKE ALL ON FUNCTION workspace_is_demo(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION workspace_is_demo(uuid,uuid) TO wsl_platform;

CREATE FUNCTION operator_workspace_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND (NEW.space_kind,NEW.operator_owner_id)
        IS DISTINCT FROM (OLD.space_kind,OLD.operator_owner_id) THEN
        RAISE EXCEPTION 'immutable workspace ownership type' USING ERRCODE='23514';
    END IF;
    IF NEW.space_kind='operator_demo' AND NOT EXISTS(SELECT 1 FROM users u
        WHERE u.id=NEW.operator_owner_id AND u.role='superadmin' AND u.tenant_id IS NULL) THEN
        RAISE EXCEPTION 'operator workspace requires platform owner' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER operator_workspace_scope BEFORE INSERT OR UPDATE ON tenants
    FOR EACH ROW EXECUTE FUNCTION operator_workspace_scope();

INSERT INTO tenants(name,owner_id,operator_owner_id,space_kind)
    SELECT left(username,80)||'的演示空间',id,id,'operator_demo' FROM users
    WHERE role='superadmin' AND NOT disabled;

CREATE FUNCTION provision_operator_workspace() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.role='user' AND EXISTS(SELECT 1 FROM tenants t WHERE t.id=NEW.tenant_id
        AND t.space_kind='operator_demo') THEN
        RAISE EXCEPTION 'operator space is owner only' USING ERRCODE='23514';
    END IF;
    IF NEW.role='superadmin' AND NOT NEW.disabled THEN
        INSERT INTO tenants(name,owner_id,operator_owner_id,space_kind)
            VALUES(left(NEW.username,80)||'的演示空间',NEW.id,NEW.id,'operator_demo')
            ON CONFLICT(operator_owner_id) WHERE space_kind='operator_demo' DO NOTHING;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER provision_operator_workspace AFTER INSERT OR UPDATE OF role,disabled,tenant_id ON users
    FOR EACH ROW EXECUTE FUNCTION provision_operator_workspace();

CREATE FUNCTION workspace_customer_owner(space uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT EXISTS(SELECT 1 FROM tenants t JOIN users u ON u.id=t.owner_id
        WHERE t.id=space AND t.space_kind='tenant_business'
        AND u.role='user' AND u.tenant_id=t.id AND u.tenant_role='owner' AND NOT u.disabled)
$$;
REVOKE ALL ON FUNCTION workspace_customer_owner(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION workspace_customer_owner(uuid) TO wsl_platform;

CREATE OR REPLACE FUNCTION group_binding_applicant_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE check_applicant boolean:=TG_OP='INSERT';
BEGIN
    IF NOT EXISTS(SELECT 1 FROM accounts a WHERE a.id=NEW.account_id AND a.platform=NEW.platform
        AND a.workspace=NEW.platform AND a.purpose='management') THEN
        RAISE EXCEPTION 'customer onboarding account scope' USING ERRCODE='23514';
    END IF;
    IF NEW.group_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM native_groups g
        WHERE g.id=NEW.group_id AND g.platform=NEW.platform) THEN
        RAISE EXCEPTION 'customer onboarding group scope' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' THEN
        IF (NEW.id,NEW.tenant_id,NEW.applicant_id,NEW.applicant_mode,NEW.platform,NEW.account_id,
            NEW.account_epoch,NEW.account_version,NEW.applicant_version,NEW.token_hash,NEW.private_token_hash,
            NEW.create_key,NEW.request_hash,NEW.created_at,NEW.expires_at)
            IS DISTINCT FROM
            (OLD.id,OLD.tenant_id,OLD.applicant_id,OLD.applicant_mode,OLD.platform,OLD.account_id,
            OLD.account_epoch,OLD.account_version,OLD.applicant_version,OLD.token_hash,OLD.private_token_hash,
            OLD.create_key,OLD.request_hash,OLD.created_at,OLD.expires_at)
            OR OLD.proof_event_id IS NOT NULL AND
                (NEW.proof_event_id,NEW.proof_sealed,NEW.proof_role,NEW.group_id,NEW.binding_version,NEW.transfer_from_tenant_id)
                IS DISTINCT FROM
                (OLD.proof_event_id,OLD.proof_sealed,OLD.proof_role,OLD.group_id,OLD.binding_version,OLD.transfer_from_tenant_id)
            OR OLD.identity_event_id IS NOT NULL AND
                (NEW.identity_event_id,NEW.identity_sealed) IS DISTINCT FROM (OLD.identity_event_id,OLD.identity_sealed) THEN
            RAISE EXCEPTION 'immutable customer challenge scope' USING ERRCODE='23514';
        END IF;
        check_applicant:=NEW.proof_event_id IS DISTINCT FROM OLD.proof_event_id
            OR NEW.identity_event_id IS DISTINCT FROM OLD.identity_event_id;
    END IF;
    IF check_applicant AND NOT EXISTS(
        SELECT 1 FROM users u JOIN tenants t ON t.id=NEW.tenant_id
        WHERE u.id=NEW.applicant_id AND NOT u.disabled AND u.access_version=NEW.applicant_version
        AND (NEW.applicant_mode='operator_demo' AND u.role='superadmin'
            AND (workspace_is_demo(t.id,u.id) OR workspace_customer_owner(t.id))
            OR NEW.applicant_mode='tenant_business' AND u.role='user' AND u.tenant_id=t.id
                AND u.tenant_role='owner' AND t.owner_id=u.id AND workspace_customer_owner(t.id))
    ) THEN
        RAISE EXCEPTION 'customer onboarding applicant scope' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION customer_reserved_binding_capacity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE used integer; held integer; account uuid; allowed integer; operator_submission boolean:=false;
BEGIN
    PERFORM pg_advisory_xact_lock(798423);
    IF NOT NEW.active THEN RETURN NEW; END IF;
    SELECT count(*) INTO used FROM group_bindings WHERE active AND group_id<>NEW.group_id;
    SELECT count(*) INTO held FROM group_binding_challenges c WHERE c.group_id IS DISTINCT FROM NEW.group_id
        AND ((c.state IN ('waiting','proven','review') AND c.reservation_until>clock_timestamp())
            OR EXISTS(SELECT 1 FROM customer_group_accept_attempts a WHERE a.challenge_id=c.id AND a.status IN ('prepared','submitting','unknown')))
        AND NOT EXISTS(SELECT 1 FROM group_bindings b WHERE b.active AND b.group_id=c.group_id);
    IF used+held>=300 THEN
        RAISE EXCEPTION 'reserved group pool full' USING ERRCODE='23514',CONSTRAINT='group_pool_capacity';
    END IF;
    SELECT EXISTS(
        SELECT 1 FROM group_binding_challenges c JOIN users u ON u.id=c.applicant_id
        JOIN tenants t ON t.id=c.tenant_id
        WHERE c.id::text=current_setting('app.group_binding_challenge',true)
        AND c.applicant_mode='operator_demo' AND c.state IN ('proven','review')
        AND c.reservation_until>clock_timestamp() AND c.proof_sealed IS NOT NULL
        AND c.proof_event_id IS NOT NULL AND c.proof_role IN ('owner','admin')
        AND c.group_id=NEW.group_id AND c.tenant_id=NEW.tenant_id AND c.account_id=NEW.primary_account_id
        AND u.role='superadmin' AND NOT u.disabled AND u.access_version=c.applicant_version
        AND (workspace_is_demo(t.id,u.id) OR workspace_customer_owner(t.id))
    ) INTO operator_submission;
    IF EXISTS(SELECT 1 FROM tenants WHERE id=NEW.tenant_id AND space_kind='operator_demo')
        AND NOT workspace_is_demo(NEW.tenant_id) THEN
        RAISE EXCEPTION 'operator workspace owner inactive' USING ERRCODE='23514';
    END IF;
    SELECT count(*) INTO held FROM group_binding_challenges c WHERE c.tenant_id=NEW.tenant_id
        AND c.group_id IS DISTINCT FROM NEW.group_id
        AND ((c.state IN ('waiting','proven','review') AND c.reservation_until>clock_timestamp())
            OR EXISTS(SELECT 1 FROM customer_group_accept_attempts a WHERE a.challenge_id=c.id AND a.status IN ('prepared','submitting','unknown')))
        AND NOT EXISTS(SELECT 1 FROM group_bindings b WHERE b.active AND b.group_id=c.group_id AND b.tenant_id=c.tenant_id);
    IF held>0 AND NOT operator_submission AND NOT workspace_is_demo(NEW.tenant_id) THEN
        SELECT p.group_limit INTO allowed FROM subscriptions s JOIN plans p ON p.id=s.plan_id
            WHERE s.tenant_id=NEW.tenant_id AND s.enabled AND s.expires_at>clock_timestamp()
            AND EXISTS(SELECT 1 FROM native_groups g WHERE g.id=NEW.group_id AND g.platform=ANY(p.allowed_platforms));
        SELECT count(*) INTO used FROM group_bindings WHERE active AND group_id<>NEW.group_id AND tenant_id=NEW.tenant_id;
        IF allowed IS NULL OR used+held>=allowed THEN
            RAISE EXCEPTION 'reserved tenant quota full' USING ERRCODE='23514',CONSTRAINT='customer_tenant_capacity';
        END IF;
    END IF;
    FOREACH account IN ARRAY ARRAY[NEW.primary_account_id,NEW.ai_account_id] LOOP
        IF account IS NULL THEN CONTINUE; END IF;
        SELECT count(*) INTO used FROM group_bindings WHERE active AND group_id<>NEW.group_id
            AND (primary_account_id=account OR ai_account_id=account);
        SELECT count(*) INTO held FROM group_binding_challenges c WHERE c.account_id=account
            AND c.group_id IS DISTINCT FROM NEW.group_id
            AND ((c.state IN ('waiting','proven','review') AND c.reservation_until>clock_timestamp())
                OR EXISTS(SELECT 1 FROM customer_group_accept_attempts a WHERE a.challenge_id=c.id AND a.status IN ('prepared','submitting','unknown')))
            AND NOT EXISTS(SELECT 1 FROM group_bindings b WHERE b.active AND b.group_id=c.group_id
                AND (b.primary_account_id=account OR b.ai_account_id=account));
        IF used+held>=30 THEN
            RAISE EXCEPTION 'reserved account full' USING ERRCODE='23514',CONSTRAINT='account_group_capacity';
        END IF;
    END LOOP;
    RETURN NEW;
END $$;
