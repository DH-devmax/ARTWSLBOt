CREATE TABLE customer_invitations (
    id uuid PRIMARY KEY,
    token_hash bytea NOT NULL UNIQUE CHECK(octet_length(token_hash)=32),
    created_by uuid NOT NULL REFERENCES users(id),
    mode text NOT NULL CHECK(mode IN ('new','existing')),
    tenant_name text NOT NULL DEFAULT '' CHECK(length(tenant_name)<=100),
    existing_user_id uuid REFERENCES users(id),
    state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','accepted','revoked')),
    accepted_user_id uuid REFERENCES users(id),
    accepted_tenant_id uuid REFERENCES tenants(id),
    accepted_at timestamptz,
    create_key text NOT NULL CHECK(length(create_key) BETWEEN 8 AND 100),
    request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '24 hours',
    CHECK(expires_at>created_at AND expires_at<=created_at+interval '24 hours 1 second'),
    CHECK((mode='new' AND existing_user_id IS NULL AND length(tenant_name)>0)
        OR (mode='existing' AND existing_user_id IS NOT NULL)),
    CHECK((state='accepted')=(accepted_user_id IS NOT NULL AND accepted_tenant_id IS NOT NULL AND accepted_at IS NOT NULL)),
    UNIQUE(created_by,create_key)
);

CREATE TABLE group_binding_challenges (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    applicant_id uuid NOT NULL REFERENCES users(id),
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    account_id uuid NOT NULL REFERENCES accounts(id),
    account_epoch bigint NOT NULL CHECK(account_epoch>0),
    account_version bigint NOT NULL CHECK(account_version>0),
    applicant_version bigint NOT NULL CHECK(applicant_version>0),
    token_hash bytea NOT NULL UNIQUE CHECK(octet_length(token_hash)=32),
    private_token_hash bytea NOT NULL UNIQUE CHECK(octet_length(private_token_hash)=32),
    create_key text NOT NULL CHECK(length(create_key) BETWEEN 8 AND 100),
    request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
    state text NOT NULL DEFAULT 'waiting' CHECK(state IN
        ('waiting','proven','review','bound','cancelled','expired','rejected')),
    group_id uuid REFERENCES native_groups(id),
    binding_version bigint NOT NULL DEFAULT 0,
    transfer_from_tenant_id uuid REFERENCES tenants(id),
    transfer_review_key text,
    proof_event_id bigint,
    proof_sealed bytea,
    identity_event_id bigint,
    identity_sealed bytea,
    proof_role text CHECK(proof_role IN ('owner','admin')),
    confirmed_at timestamptz,
    reviewed_by uuid REFERENCES users(id),
    reviewed_at timestamptz,
    confirm_key text,
    blocked_reason text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    last_scan_at timestamptz NOT NULL DEFAULT '-infinity',
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '10 minutes',
    reservation_until timestamptz NOT NULL DEFAULT clock_timestamp()+interval '10 minutes',
    CHECK(expires_at>created_at AND expires_at<=created_at+interval '10 minutes 1 second'),
    CHECK(reservation_until<=created_at+interval '24 hours 10 minutes'),
    CHECK(proof_event_id IS NULL OR (proof_sealed IS NOT NULL AND group_id IS NOT NULL AND proof_role IS NOT NULL)),
    CHECK((identity_event_id IS NULL)=(identity_sealed IS NULL)),
    CHECK(transfer_from_tenant_id IS NULL OR (transfer_from_tenant_id<>tenant_id AND proof_role='owner' AND binding_version>0)),
    UNIQUE(applicant_id,create_key),
    UNIQUE(account_id,proof_event_id)
);
CREATE UNIQUE INDEX group_binding_one_live_actor ON group_binding_challenges(applicant_id,platform)
    WHERE state IN ('waiting','proven','review');
CREATE UNIQUE INDEX group_binding_one_live_group ON group_binding_challenges(group_id)
    WHERE group_id IS NOT NULL AND state IN ('waiting','proven','review');
CREATE INDEX group_binding_scan ON group_binding_challenges(state,last_scan_at,id);
CREATE INDEX group_binding_reservations ON group_binding_challenges(account_id,reservation_until)
    WHERE state IN ('waiting','proven','review');
CREATE TABLE group_binding_observations (
    challenge_id uuid NOT NULL REFERENCES group_binding_challenges(id) ON DELETE CASCADE,
    event_id bigint NOT NULL,
    PRIMARY KEY(challenge_id,event_id)
);

CREATE TABLE customer_group_accept_attempts (
    id uuid PRIMARY KEY,
    challenge_id uuid NOT NULL UNIQUE REFERENCES group_binding_challenges(id),
    actor_id uuid NOT NULL REFERENCES users(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    account_id uuid NOT NULL REFERENCES accounts(id),
    group_id uuid NOT NULL REFERENCES native_groups(id),
    account_epoch bigint NOT NULL,
    account_version bigint NOT NULL,
    request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
    payload_sealed bytea NOT NULL,
    idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 8 AND 100),
    status text NOT NULL DEFAULT 'prepared' CHECK(status IN ('prepared','submitting','succeeded','failed','unknown')),
    result_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE(actor_id,idempotency_key)
);
CREATE UNIQUE INDEX customer_accept_one_unknown_account ON customer_group_accept_attempts(account_id)
    WHERE status IN ('prepared','submitting','unknown');
CREATE TABLE group_binding_ownership (
    group_id uuid PRIMARY KEY REFERENCES native_groups(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    owner_identity_hash bytea NOT NULL CHECK(octet_length(owner_identity_hash)=32),
    owner_identity_sealed bytea NOT NULL,
    applicant_identity_sealed bytea NOT NULL,
    applicant_role text NOT NULL CHECK(applicant_role IN ('owner','admin')),
    robot_role text NOT NULL CHECK(robot_role IN ('owner','admin','member')),
    proof_challenge_id uuid NOT NULL REFERENCES group_binding_challenges(id),
    proof_event_id bigint NOT NULL,
    state text NOT NULL DEFAULT 'active' CHECK(state IN ('active','paused')),
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    verified_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
ALTER TABLE group_claims ADD COLUMN onboarding_challenge_id uuid UNIQUE REFERENCES group_binding_challenges(id);
ALTER TABLE group_claims ADD COLUMN proof_method text NOT NULL DEFAULT 'manual'
    CHECK(proof_method IN ('manual','authenticated_message'));
ALTER TABLE group_claims ADD CONSTRAINT claim_proof_origin
    CHECK((proof_method='manual')=(onboarding_challenge_id IS NULL));
ALTER TABLE claim_codes ADD COLUMN target_tenant_id uuid REFERENCES tenants(id);

-- Retain immutable finance/activity ownership when a reviewed group moves.
-- Current-binding authorization continues to govern new writes and reads.
CREATE TABLE group_binding_tenant_history (
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    group_id uuid NOT NULL REFERENCES native_groups(id) ON DELETE CASCADE,
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(tenant_id,group_id)
);
INSERT INTO group_binding_tenant_history(tenant_id,group_id)
    SELECT tenant_id,group_id FROM group_bindings;
ALTER TABLE group_bindings ADD COLUMN data_since timestamptz NOT NULL DEFAULT '-infinity';
ALTER TABLE ledger_entries DROP CONSTRAINT ledger_entries_tenant_id_group_id_fkey;
ALTER TABLE ledger_entries ADD CONSTRAINT ledger_entries_tenant_id_group_id_fkey
    FOREIGN KEY(tenant_id,group_id) REFERENCES group_binding_tenant_history(tenant_id,group_id);
ALTER TABLE activities DROP CONSTRAINT activities_tenant_id_group_id_fkey;
ALTER TABLE activities ADD CONSTRAINT activities_tenant_id_group_id_fkey
    FOREIGN KEY(tenant_id,group_id) REFERENCES group_binding_tenant_history(tenant_id,group_id);
ALTER TABLE operations DROP CONSTRAINT operation_scope;
ALTER TABLE operations ADD CONSTRAINT operation_scope
    FOREIGN KEY(tenant_id,group_id) REFERENCES group_binding_tenant_history(tenant_id,group_id);

CREATE FUNCTION customer_binding_tenant_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO group_binding_tenant_history(tenant_id,group_id) VALUES(NEW.tenant_id,NEW.group_id)
        ON CONFLICT DO NOTHING;
    RETURN NEW;
END $$;
CREATE TRIGGER customer_binding_tenant_history AFTER INSERT OR UPDATE OF tenant_id ON group_bindings
    FOR EACH ROW EXECUTE FUNCTION customer_binding_tenant_history();

CREATE FUNCTION customer_current_bound_group() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.tenant_id IS NOT NULL AND NOT EXISTS(
        SELECT 1 FROM group_bindings b WHERE b.group_id=NEW.group_id AND b.tenant_id=NEW.tenant_id) THEN
        RAISE EXCEPTION 'current group tenant mismatch' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER customer_ledger_current_group BEFORE INSERT ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION customer_current_bound_group();
CREATE TRIGGER customer_activity_current_group BEFORE INSERT ON activities
    FOR EACH ROW EXECUTE FUNCTION customer_current_bound_group();
CREATE TRIGGER customer_operation_current_group BEFORE INSERT ON operations
    FOR EACH ROW EXECUTE FUNCTION customer_current_bound_group();

CREATE TABLE customer_group_transfers (
    id uuid PRIMARY KEY,
    challenge_id uuid NOT NULL UNIQUE REFERENCES group_binding_challenges(id),
    group_id uuid NOT NULL REFERENCES native_groups(id),
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    source_tenant_id uuid NOT NULL REFERENCES tenants(id),
    target_tenant_id uuid NOT NULL REFERENCES tenants(id),
    reviewed_by uuid NOT NULL REFERENCES users(id),
    review_key text NOT NULL CHECK(length(review_key) BETWEEN 8 AND 100),
    source_binding_version bigint NOT NULL CHECK(source_binding_version>0),
    source_binding_epoch bigint NOT NULL CHECK(source_binding_epoch>0),
    archived_settings_sealed bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK(source_tenant_id<>target_tenant_id),
    UNIQUE(reviewed_by,review_key)
);
CREATE FUNCTION customer_transfer_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP<>'INSERT' OR NOT EXISTS(SELECT 1 FROM group_binding_challenges c
        JOIN group_bindings b ON b.group_id=c.group_id JOIN users u ON u.id=NEW.reviewed_by
        WHERE c.id=NEW.challenge_id AND c.state='review' AND c.proof_role='owner'
        AND c.platform=NEW.platform AND c.group_id=NEW.group_id AND c.tenant_id=NEW.target_tenant_id
        AND c.transfer_from_tenant_id=NEW.source_tenant_id AND b.tenant_id=NEW.source_tenant_id
        AND b.version=NEW.source_binding_version AND b.epoch=NEW.source_binding_epoch
        AND c.binding_version=b.version AND u.role='superadmin' AND NOT u.disabled) THEN
        RAISE EXCEPTION 'immutable or invalid customer transfer' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER customer_transfer_scope BEFORE INSERT OR UPDATE OR DELETE ON customer_group_transfers
    FOR EACH ROW EXECUTE FUNCTION customer_transfer_scope();

CREATE FUNCTION customer_onboarding_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='customer_invitations' THEN
        IF TG_OP='UPDATE' AND (NEW.id,NEW.token_hash,NEW.created_by,NEW.mode,NEW.tenant_name,
            NEW.existing_user_id,NEW.create_key,NEW.request_hash,NEW.created_at,NEW.expires_at)
            IS DISTINCT FROM (OLD.id,OLD.token_hash,OLD.created_by,OLD.mode,OLD.tenant_name,
            OLD.existing_user_id,OLD.create_key,OLD.request_hash,OLD.created_at,OLD.expires_at) THEN
            RAISE EXCEPTION 'immutable customer invitation' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM accounts a WHERE a.id=NEW.account_id AND a.platform=NEW.platform
        AND a.workspace=NEW.platform AND a.purpose='management') THEN
        RAISE EXCEPTION 'customer onboarding account scope' USING ERRCODE='23514';
    END IF;
    IF NEW.group_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM native_groups g
        WHERE g.id=NEW.group_id AND g.platform=NEW.platform) THEN
        RAISE EXCEPTION 'customer onboarding group scope' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME='group_binding_challenges' THEN
        IF NOT EXISTS(SELECT 1 FROM users u WHERE u.id=NEW.applicant_id AND u.tenant_id=NEW.tenant_id AND u.role='user') THEN
            RAISE EXCEPTION 'customer onboarding applicant scope' USING ERRCODE='23514';
        END IF;
        IF TG_OP='UPDATE' AND ((NEW.id,NEW.tenant_id,NEW.applicant_id,NEW.platform,NEW.account_id,
            NEW.account_epoch,NEW.account_version,NEW.applicant_version,NEW.token_hash,NEW.private_token_hash,
            NEW.create_key,NEW.request_hash,NEW.created_at,NEW.expires_at)
            IS DISTINCT FROM (OLD.id,OLD.tenant_id,OLD.applicant_id,OLD.platform,OLD.account_id,
            OLD.account_epoch,OLD.account_version,OLD.applicant_version,OLD.token_hash,OLD.private_token_hash,
            OLD.create_key,OLD.request_hash,OLD.created_at,OLD.expires_at)
            OR OLD.proof_event_id IS NOT NULL AND (NEW.proof_event_id,NEW.proof_sealed,NEW.proof_role,NEW.group_id,
                NEW.binding_version,NEW.transfer_from_tenant_id)
                IS DISTINCT FROM (OLD.proof_event_id,OLD.proof_sealed,OLD.proof_role,OLD.group_id,
                OLD.binding_version,OLD.transfer_from_tenant_id)
            OR OLD.identity_event_id IS NOT NULL AND (NEW.identity_event_id,NEW.identity_sealed)
                IS DISTINCT FROM (OLD.identity_event_id,OLD.identity_sealed)) THEN
            RAISE EXCEPTION 'immutable customer challenge scope' USING ERRCODE='23514';
        END IF;
    ELSE
        IF NOT EXISTS(SELECT 1 FROM group_binding_challenges c WHERE c.id=NEW.challenge_id
            AND (c.tenant_id,c.applicant_id,c.platform,c.account_id,c.account_epoch,c.account_version,c.group_id)
                =(NEW.tenant_id,NEW.actor_id,NEW.platform,NEW.account_id,NEW.account_epoch,NEW.account_version,NEW.group_id)) THEN
            RAISE EXCEPTION 'customer accept challenge scope' USING ERRCODE='23514';
        END IF;
        IF TG_OP='UPDATE' AND (NEW.id,NEW.challenge_id,NEW.actor_id,NEW.tenant_id,NEW.platform,
            NEW.account_id,NEW.group_id,NEW.account_epoch,NEW.account_version,NEW.request_hash,
            NEW.payload_sealed,NEW.idempotency_key,NEW.created_at,NEW.expires_at)
            IS DISTINCT FROM (OLD.id,OLD.challenge_id,OLD.actor_id,OLD.tenant_id,OLD.platform,
            OLD.account_id,OLD.group_id,OLD.account_epoch,OLD.account_version,OLD.request_hash,
            OLD.payload_sealed,OLD.idempotency_key,OLD.created_at,OLD.expires_at) THEN
            RAISE EXCEPTION 'immutable customer accept attempt' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER customer_invitation_scope BEFORE INSERT OR UPDATE ON customer_invitations
    FOR EACH ROW EXECUTE FUNCTION customer_onboarding_scope();
CREATE TRIGGER group_binding_challenge_scope BEFORE INSERT OR UPDATE ON group_binding_challenges
    FOR EACH ROW EXECUTE FUNCTION customer_onboarding_scope();
CREATE TRIGGER customer_accept_scope BEFORE INSERT OR UPDATE ON customer_group_accept_attempts
    FOR EACH ROW EXECUTE FUNCTION customer_onboarding_scope();

CREATE FUNCTION customer_ownership_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM group_binding_challenges c JOIN group_bindings b ON b.group_id=c.group_id
        WHERE c.id=NEW.proof_challenge_id AND c.group_id=NEW.group_id AND c.tenant_id=NEW.tenant_id
        AND c.platform=NEW.platform AND c.proof_event_id=NEW.proof_event_id AND c.proof_role=NEW.applicant_role
        AND c.proof_sealed IS NOT NULL AND b.tenant_id=NEW.tenant_id) THEN
        RAISE EXCEPTION 'ownership proof scope mismatch' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER customer_ownership_scope BEFORE INSERT OR UPDATE ON group_binding_ownership
    FOR EACH ROW EXECUTE FUNCTION customer_ownership_scope();

-- Existing assignment, legacy claims and tests share the same capacity lock and
-- cannot consume a slot reserved by an in-flight customer onboarding attempt.
CREATE FUNCTION customer_reserved_binding_capacity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE used integer; held integer; account uuid; allowed integer;
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
    SELECT count(*) INTO held FROM group_binding_challenges c WHERE c.tenant_id=NEW.tenant_id
        AND c.group_id IS DISTINCT FROM NEW.group_id
        AND ((c.state IN ('waiting','proven','review') AND c.reservation_until>clock_timestamp())
            OR EXISTS(SELECT 1 FROM customer_group_accept_attempts a WHERE a.challenge_id=c.id AND a.status IN ('prepared','submitting','unknown')))
        AND NOT EXISTS(SELECT 1 FROM group_bindings b WHERE b.active AND b.group_id=c.group_id AND b.tenant_id=c.tenant_id);
    IF held>0 THEN
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
CREATE TRIGGER customer_reserved_capacity BEFORE INSERT OR UPDATE OF active,primary_account_id,ai_account_id ON group_bindings
    FOR EACH ROW EXECUTE FUNCTION customer_reserved_binding_capacity();

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['customer_invitations','group_binding_challenges',
        'group_binding_observations','customer_group_accept_attempts','group_binding_ownership',
        'group_binding_tenant_history','customer_group_transfers'] LOOP
        EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I TO wsl_platform',t);
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
        EXECUTE format('CREATE POLICY platform_only ON %I USING(current_user=''wsl_platform'') WITH CHECK(current_user=''wsl_platform'')',t);
    END LOOP;
END $$;
