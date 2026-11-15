ALTER TABLE group_binding_challenges ADD COLUMN friend_token_hash bytea;
ALTER TABLE group_binding_challenges ADD COLUMN friend_text_sealed bytea;
ALTER TABLE group_binding_challenges ADD COLUMN identity_friend_proof_id uuid;
ALTER TABLE group_binding_challenges ADD CONSTRAINT group_binding_friend_challenge_shape CHECK(
    (friend_token_hash IS NULL AND friend_text_sealed IS NULL)
    OR (friend_token_hash IS NOT NULL AND octet_length(friend_token_hash)=32
        AND friend_text_sealed IS NOT NULL AND octet_length(friend_text_sealed)>0));
CREATE UNIQUE INDEX group_binding_friend_token ON group_binding_challenges(friend_token_hash)
    WHERE friend_token_hash IS NOT NULL;

-- Account scope is explicit because no group has been proven at this point.
-- Absence is closed; neither debug grants nor business group gates admit this.
CREATE TABLE group_binding_friend_gates (
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    account_id uuid NOT NULL REFERENCES accounts(id),
    state text NOT NULL DEFAULT 'closed' CHECK(state IN ('closed','open')),
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    evidence_refs text[] NOT NULL DEFAULT '{}',
    updated_by uuid NOT NULL REFERENCES users(id),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK(state='closed' OR cardinality(evidence_refs)>0),
    PRIMARY KEY(platform,account_id)
);

CREATE TABLE group_binding_friend_attempts (
    id uuid PRIMARY KEY,
    challenge_id uuid NOT NULL UNIQUE REFERENCES group_binding_challenges(id),
    actor_id uuid NOT NULL REFERENCES users(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    account_id uuid NOT NULL REFERENCES accounts(id),
    account_epoch bigint NOT NULL CHECK(account_epoch>0),
    account_version bigint NOT NULL CHECK(account_version>0),
    applicant_version bigint NOT NULL CHECK(applicant_version>0),
    challenge_version bigint NOT NULL CHECK(challenge_version>0),
    gate_version bigint NOT NULL CHECK(gate_version>0),
    request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
    payload_sealed bytea NOT NULL CHECK(octet_length(payload_sealed)>0),
    idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 8 AND 100),
    attempt_budget smallint NOT NULL DEFAULT 1 CHECK(attempt_budget=1),
    attempts_used smallint NOT NULL DEFAULT 0 CHECK(attempts_used BETWEEN 0 AND 1),
    status text NOT NULL DEFAULT 'prepared'
        CHECK(status IN ('prepared','submitting','succeeded','failed','unknown')),
    result_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK(expires_at>created_at AND expires_at<=created_at+interval '10 minutes 1 second'),
    CHECK(status NOT IN ('submitting','succeeded') OR attempts_used=1),
    UNIQUE(actor_id,idempotency_key)
);
CREATE UNIQUE INDEX group_binding_friend_one_unresolved_account ON group_binding_friend_attempts(account_id)
    WHERE status IN ('prepared','submitting','unknown');

CREATE FUNCTION group_binding_friend_pending(challenge uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT EXISTS(SELECT 1 FROM group_binding_friend_attempts
        WHERE challenge_id=challenge AND status IN ('prepared','submitting','unknown'))
$$;
REVOKE ALL ON FUNCTION group_binding_friend_pending(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION group_binding_friend_pending(uuid) TO wsl_platform;

-- This proof records a real friend application, never a fabricated ingress ID.
-- All native selectors, native sender and full connection evidence stay sealed.
CREATE TABLE group_binding_friend_proofs (
    id uuid PRIMARY KEY,
    attempt_id uuid NOT NULL UNIQUE REFERENCES group_binding_friend_attempts(id),
    challenge_id uuid NOT NULL UNIQUE REFERENCES group_binding_challenges(id),
    actor_id uuid NOT NULL REFERENCES users(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    account_id uuid NOT NULL REFERENCES accounts(id),
    account_epoch bigint NOT NULL CHECK(account_epoch>0),
    account_version bigint NOT NULL CHECK(account_version>0),
    applicant_version bigint NOT NULL CHECK(applicant_version>0),
    challenge_version bigint NOT NULL CHECK(challenge_version>0),
    source text NOT NULL DEFAULT 'friend_application' CHECK(source='friend_application'),
    native_sender_sealed bytea NOT NULL CHECK(octet_length(native_sender_sealed)>0),
    evidence_sealed bytea NOT NULL CHECK(octet_length(evidence_sealed)>0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
ALTER TABLE group_binding_challenges ADD CONSTRAINT group_binding_identity_friend_proof
    FOREIGN KEY(identity_friend_proof_id) REFERENCES group_binding_friend_proofs(id);
CREATE UNIQUE INDEX group_binding_one_friend_identity ON group_binding_challenges(identity_friend_proof_id)
    WHERE identity_friend_proof_id IS NOT NULL;

DO $$
DECLARE identity_check text;
BEGIN
    SELECT conname INTO STRICT identity_check FROM pg_constraint
        WHERE conrelid='group_binding_challenges'::regclass AND contype='c'
        AND pg_get_constraintdef(oid) LIKE '%identity_event_id%'
        AND pg_get_constraintdef(oid) LIKE '%identity_sealed%';
    EXECUTE format('ALTER TABLE group_binding_challenges DROP CONSTRAINT %I',identity_check);
END $$;
ALTER TABLE group_binding_challenges ADD CONSTRAINT group_binding_identity_source CHECK(
    (identity_sealed IS NULL AND identity_event_id IS NULL AND identity_friend_proof_id IS NULL)
    OR (identity_sealed IS NOT NULL AND
        ((identity_event_id IS NOT NULL AND identity_friend_proof_id IS NULL)
            OR (identity_event_id IS NULL AND identity_friend_proof_id IS NOT NULL))));

CREATE FUNCTION group_binding_friend_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'immutable onboarding friend history' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME='group_binding_friend_gates' THEN
        IF TG_OP='UPDATE' AND ((NEW.platform,NEW.account_id) IS DISTINCT FROM (OLD.platform,OLD.account_id)
            OR NEW.version<>OLD.version+1) THEN
            RAISE EXCEPTION 'immutable onboarding friend gate scope' USING ERRCODE='23514';
        END IF;
        IF NOT EXISTS(SELECT 1 FROM accounts a JOIN users u ON u.id=NEW.updated_by
            WHERE a.id=NEW.account_id AND a.platform=NEW.platform AND a.workspace=NEW.platform
            AND a.purpose='management' AND u.role='superadmin' AND NOT u.disabled) THEN
            RAISE EXCEPTION 'onboarding friend gate scope' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP='UPDATE' THEN
        IF TG_TABLE_NAME='group_binding_friend_proofs' THEN
            RAISE EXCEPTION 'immutable onboarding friend proof' USING ERRCODE='23514';
        END IF;
        IF (NEW.id,NEW.challenge_id,NEW.actor_id,NEW.tenant_id,NEW.platform,NEW.account_id,NEW.account_epoch,
            NEW.account_version,NEW.applicant_version,NEW.challenge_version,NEW.gate_version,NEW.request_hash,
            NEW.payload_sealed,NEW.idempotency_key,NEW.attempt_budget,NEW.created_at,NEW.expires_at)
            IS DISTINCT FROM
            (OLD.id,OLD.challenge_id,OLD.actor_id,OLD.tenant_id,OLD.platform,OLD.account_id,OLD.account_epoch,
            OLD.account_version,OLD.applicant_version,OLD.challenge_version,OLD.gate_version,OLD.request_hash,
            OLD.payload_sealed,OLD.idempotency_key,OLD.attempt_budget,OLD.created_at,OLD.expires_at)
            OR NEW.attempts_used<OLD.attempts_used
            OR OLD.status IN ('succeeded','failed') AND (NEW.status,NEW.result_code,NEW.attempts_used)
                IS DISTINCT FROM (OLD.status,OLD.result_code,OLD.attempts_used)
            OR OLD.status='unknown' AND NEW.status NOT IN ('unknown','succeeded','failed')
            OR OLD.status='submitting' AND NEW.status='prepared' THEN
            RAISE EXCEPTION 'immutable onboarding friend attempt' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM group_binding_challenges c
        WHERE c.id=NEW.challenge_id AND c.state='waiting' AND c.identity_sealed IS NULL
        AND c.expires_at>clock_timestamp() AND c.friend_token_hash IS NOT NULL AND c.friend_text_sealed IS NOT NULL
        AND (c.applicant_id,c.tenant_id,c.platform,c.account_id,c.account_epoch,c.account_version,c.applicant_version,c.version)
            =(NEW.actor_id,NEW.tenant_id,NEW.platform,NEW.account_id,NEW.account_epoch,NEW.account_version,NEW.applicant_version,NEW.challenge_version)) THEN
        RAISE EXCEPTION 'onboarding friend challenge scope' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME='group_binding_friend_attempts' THEN
        IF NEW.status<>'prepared' OR NEW.attempts_used<>0 OR NOT EXISTS(
            SELECT 1 FROM group_binding_friend_gates g WHERE g.platform=NEW.platform AND g.account_id=NEW.account_id
            AND g.state='open' AND g.version=NEW.gate_version) THEN
            RAISE EXCEPTION 'onboarding friend admission closed' USING ERRCODE='23514';
        END IF;
    ELSE
        IF NOT EXISTS(SELECT 1 FROM group_binding_friend_attempts a
            WHERE a.id=NEW.attempt_id AND a.status='prepared'
            AND (a.challenge_id,a.actor_id,a.tenant_id,a.platform,a.account_id,a.account_epoch,a.account_version,a.applicant_version,a.challenge_version)
                =(NEW.challenge_id,NEW.actor_id,NEW.tenant_id,NEW.platform,NEW.account_id,NEW.account_epoch,NEW.account_version,NEW.applicant_version,NEW.challenge_version)) THEN
            RAISE EXCEPTION 'onboarding friend proof scope' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER group_binding_friend_gate_scope BEFORE INSERT OR UPDATE OR DELETE ON group_binding_friend_gates
    FOR EACH ROW EXECUTE FUNCTION group_binding_friend_scope();
CREATE TRIGGER group_binding_friend_attempt_scope BEFORE INSERT OR UPDATE OR DELETE ON group_binding_friend_attempts
    FOR EACH ROW EXECUTE FUNCTION group_binding_friend_scope();
CREATE TRIGGER group_binding_friend_proof_scope BEFORE INSERT OR UPDATE OR DELETE ON group_binding_friend_proofs
    FOR EACH ROW EXECUTE FUNCTION group_binding_friend_scope();

-- Preserve migration 056's genuine operator spaces and existing customer work.
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
        IF NEW.state IS DISTINCT FROM OLD.state AND NEW.state IN ('cancelled','expired') THEN
            -- Lock in the same order as the native fence: challenge, attempt.
            -- A prepared operation has no native effect and can be stopped.
            PERFORM 1 FROM group_binding_friend_attempts WHERE challenge_id=NEW.id FOR UPDATE;
            IF EXISTS(SELECT 1 FROM group_binding_friend_attempts WHERE challenge_id=NEW.id
                AND status IN ('submitting','unknown')) THEN
                RAISE EXCEPTION 'onboarding friend operation unresolved'
                    USING ERRCODE='23514',CONSTRAINT='group_binding_friend_operation_unresolved';
            END IF;
            UPDATE group_binding_friend_attempts SET status='failed',attempts_used=1,
                result_code=CASE WHEN NEW.state='cancelled' THEN 'group_binding_challenge_cancelled'
                    ELSE 'group_binding_challenge_expired' END,updated_at=clock_timestamp()
                WHERE challenge_id=NEW.id AND status='prepared';
        END IF;
        IF (NEW.id,NEW.tenant_id,NEW.applicant_id,NEW.applicant_mode,NEW.platform,NEW.account_id,
            NEW.account_epoch,NEW.account_version,NEW.applicant_version,NEW.token_hash,NEW.private_token_hash,
            NEW.create_key,NEW.request_hash,NEW.created_at,NEW.expires_at)
            IS DISTINCT FROM
            (OLD.id,OLD.tenant_id,OLD.applicant_id,OLD.applicant_mode,OLD.platform,OLD.account_id,
            OLD.account_epoch,OLD.account_version,OLD.applicant_version,OLD.token_hash,OLD.private_token_hash,
            OLD.create_key,OLD.request_hash,OLD.created_at,OLD.expires_at)
            OR OLD.friend_token_hash IS NOT NULL AND
                (NEW.friend_token_hash,NEW.friend_text_sealed) IS DISTINCT FROM (OLD.friend_token_hash,OLD.friend_text_sealed)
            OR OLD.proof_event_id IS NOT NULL AND
                (NEW.proof_event_id,NEW.proof_sealed,NEW.proof_role,NEW.group_id,NEW.binding_version,NEW.transfer_from_tenant_id)
                IS DISTINCT FROM
                (OLD.proof_event_id,OLD.proof_sealed,OLD.proof_role,OLD.group_id,OLD.binding_version,OLD.transfer_from_tenant_id)
            OR OLD.identity_sealed IS NOT NULL AND
                (NEW.identity_event_id,NEW.identity_friend_proof_id,NEW.identity_sealed)
                IS DISTINCT FROM (OLD.identity_event_id,OLD.identity_friend_proof_id,OLD.identity_sealed) THEN
            RAISE EXCEPTION 'immutable customer challenge scope' USING ERRCODE='23514';
        END IF;
        check_applicant:=NEW.proof_event_id IS DISTINCT FROM OLD.proof_event_id
            OR NEW.identity_sealed IS DISTINCT FROM OLD.identity_sealed
            OR NEW.friend_token_hash IS DISTINCT FROM OLD.friend_token_hash;
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
    IF NEW.identity_friend_proof_id IS NOT NULL AND (TG_OP='INSERT'
        OR NEW.identity_friend_proof_id IS DISTINCT FROM OLD.identity_friend_proof_id) THEN
        IF NOT EXISTS(SELECT 1 FROM group_binding_friend_proofs p JOIN group_binding_friend_attempts a ON a.id=p.attempt_id
            WHERE p.id=NEW.identity_friend_proof_id AND p.challenge_id=NEW.id AND a.status IN ('submitting','unknown','succeeded')
            AND (p.actor_id,p.tenant_id,p.platform,p.account_id,p.account_epoch,p.account_version,p.applicant_version)
                =(NEW.applicant_id,NEW.tenant_id,NEW.platform,NEW.account_id,NEW.account_epoch,NEW.account_version,NEW.applicant_version)
            AND p.source='friend_application' AND p.challenge_version=NEW.version-1) THEN
            RAISE EXCEPTION 'onboarding friend identity source mismatch' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;

-- Keep the same 056 workspace rules and shared capacity lock. An unknown
-- friend submission retains its challenge slot even after reservation_until.
CREATE OR REPLACE FUNCTION customer_reserved_binding_capacity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE used integer; held integer; account uuid; allowed integer; operator_submission boolean:=false;
BEGIN
    PERFORM pg_advisory_xact_lock(798423);
    IF NOT NEW.active THEN RETURN NEW; END IF;
    SELECT count(*) INTO used FROM group_bindings WHERE active AND group_id<>NEW.group_id;
    SELECT count(*) INTO held FROM group_binding_challenges c WHERE c.group_id IS DISTINCT FROM NEW.group_id
        AND ((c.state IN ('waiting','proven','review') AND c.reservation_until>clock_timestamp())
            OR EXISTS(SELECT 1 FROM customer_group_accept_attempts a WHERE a.challenge_id=c.id AND a.status IN ('prepared','submitting','unknown'))
            OR group_binding_friend_pending(c.id))
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
            OR EXISTS(SELECT 1 FROM customer_group_accept_attempts a WHERE a.challenge_id=c.id AND a.status IN ('prepared','submitting','unknown'))
            OR group_binding_friend_pending(c.id))
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
                OR EXISTS(SELECT 1 FROM customer_group_accept_attempts a WHERE a.challenge_id=c.id AND a.status IN ('prepared','submitting','unknown'))
                OR group_binding_friend_pending(c.id))
            AND NOT EXISTS(SELECT 1 FROM group_bindings b WHERE b.active AND b.group_id=c.group_id
                AND (b.primary_account_id=account OR b.ai_account_id=account));
        IF used+held>=30 THEN
            RAISE EXCEPTION 'reserved account full' USING ERRCODE='23514',CONSTRAINT='account_group_capacity';
        END IF;
    END LOOP;
    RETURN NEW;
END $$;

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['group_binding_friend_gates','group_binding_friend_attempts','group_binding_friend_proofs'] LOOP
        EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I TO wsl_platform',t);
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
        EXECUTE format('CREATE POLICY platform_only ON %I USING(current_user=''wsl_platform'') WITH CHECK(current_user=''wsl_platform'')',t);
    END LOOP;
END $$;
