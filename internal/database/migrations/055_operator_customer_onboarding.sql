-- The website operator remains the applicant; the selected real customer owns
-- the resulting resource. Existing challenges keep their customer semantics.
ALTER TABLE group_binding_challenges ADD COLUMN applicant_mode text NOT NULL DEFAULT 'tenant_business'
    CHECK(applicant_mode IN ('tenant_business','operator_demo'));

CREATE FUNCTION group_binding_applicant_scope() RETURNS trigger LANGUAGE plpgsql AS $$
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
    -- Revoked applicants cannot add a proof. Expiry/recovery can
    -- still mark old records without depending on a surviving website role.
    IF check_applicant THEN
        IF NOT EXISTS(
            SELECT 1 FROM users u JOIN tenants t ON t.id=NEW.tenant_id
            JOIN users owner ON owner.id=t.owner_id AND owner.tenant_id=t.id
                AND owner.role='user' AND owner.tenant_role='owner' AND NOT owner.disabled
            WHERE u.id=NEW.applicant_id AND NOT u.disabled AND u.access_version=NEW.applicant_version
            AND (NEW.applicant_mode='operator_demo' AND u.role='superadmin'
                OR NEW.applicant_mode='tenant_business' AND u.role='user' AND u.tenant_id=t.id
                    AND u.tenant_role='owner' AND t.owner_id=u.id)
        ) THEN
            RAISE EXCEPTION 'customer onboarding applicant scope' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER group_binding_challenge_scope ON group_binding_challenges;
CREATE TRIGGER group_binding_challenge_scope BEFORE INSERT OR UPDATE ON group_binding_challenges
    FOR EACH ROW EXECUTE FUNCTION group_binding_applicant_scope();

-- Only the explicit, immutable operator challenge named by the control
-- transaction replaces the commercial check. Pool reservations stay shared.
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
        JOIN tenants t ON t.id=c.tenant_id JOIN users owner ON owner.id=t.owner_id
        WHERE c.id::text=current_setting('app.group_binding_challenge',true)
        AND c.applicant_mode='operator_demo' AND c.state IN ('proven','review')
        AND c.reservation_until>clock_timestamp() AND c.proof_sealed IS NOT NULL
        AND c.proof_event_id IS NOT NULL AND c.proof_role IN ('owner','admin')
        AND c.group_id=NEW.group_id AND c.tenant_id=NEW.tenant_id AND c.account_id=NEW.primary_account_id
        AND u.role='superadmin' AND NOT u.disabled AND u.access_version=c.applicant_version
        AND owner.tenant_id=t.id AND owner.role='user' AND owner.tenant_role='owner' AND NOT owner.disabled
    ) INTO operator_submission;
    SELECT count(*) INTO held FROM group_binding_challenges c WHERE c.tenant_id=NEW.tenant_id
        AND c.group_id IS DISTINCT FROM NEW.group_id
        AND ((c.state IN ('waiting','proven','review') AND c.reservation_until>clock_timestamp())
            OR EXISTS(SELECT 1 FROM customer_group_accept_attempts a WHERE a.challenge_id=c.id AND a.status IN ('prepared','submitting','unknown')))
        AND NOT EXISTS(SELECT 1 FROM group_bindings b WHERE b.active AND b.group_id=c.group_id AND b.tenant_id=c.tenant_id);
    IF held>0 AND NOT operator_submission THEN
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
