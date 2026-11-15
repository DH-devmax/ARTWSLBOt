-- Outgoing contact requests never establish native identity or group ownership.
-- Existing incoming attempts retain their immutable history and reconciliation.
ALTER TABLE group_binding_challenges ADD COLUMN friend_flow text NOT NULL DEFAULT 'legacy'
    CHECK(friend_flow IN ('legacy','outbound'));
ALTER TABLE group_binding_challenges ALTER COLUMN friend_flow SET DEFAULT 'outbound';

CREATE TABLE group_binding_outbound_gates (
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    account_id uuid NOT NULL REFERENCES accounts(id),
    state text NOT NULL CHECK(state IN ('closed','open')),
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    evidence_refs text[] NOT NULL DEFAULT '{}',
    updated_by uuid NOT NULL REFERENCES users(id),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(platform,account_id)
);

CREATE TABLE group_binding_outbound_attempts (
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
    payload_sealed bytea NOT NULL CHECK(octet_length(payload_sealed) BETWEEN 32 AND 16384),
    prepare_key text NOT NULL CHECK(length(prepare_key) BETWEEN 8 AND 100),
    send_key text CHECK(length(send_key) BETWEEN 8 AND 100),
    status text NOT NULL CHECK(status IN ('prepared','submitting','submitted','friends','failed','unknown')),
    attempts_used integer NOT NULL DEFAULT 0 CHECK(attempts_used BETWEEN 0 AND 1),
    result_code text NOT NULL DEFAULT '' CHECK(length(result_code)<=120),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE(actor_id,prepare_key),
    UNIQUE(actor_id,send_key),
    CHECK(expires_at>created_at AND expires_at<=created_at+interval '10 minutes 1 second'),
    CHECK(status NOT IN ('submitting','submitted','unknown') OR attempts_used=1)
);
CREATE UNIQUE INDEX group_binding_outbound_unresolved ON group_binding_outbound_attempts(account_id)
    WHERE status IN ('submitting','unknown');
CREATE INDEX group_binding_outbound_rate ON group_binding_outbound_attempts(account_id,created_at)
    WHERE attempts_used=1;
CREATE INDEX group_binding_outbound_actor_rate ON group_binding_outbound_attempts(actor_id,created_at)
    WHERE attempts_used=1;

CREATE OR REPLACE FUNCTION group_binding_friend_pending(challenge uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS(SELECT 1 FROM group_binding_friend_attempts WHERE challenge_id=challenge
        AND status IN ('prepared','submitting','unknown'))
        OR EXISTS(SELECT 1 FROM group_binding_outbound_attempts WHERE challenge_id=challenge
            AND status IN ('submitting','unknown'))
$$;

CREATE FUNCTION group_binding_outbound_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'immutable outbound friend history' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' THEN
        IF (NEW.id,NEW.challenge_id,NEW.actor_id,NEW.tenant_id,NEW.platform,NEW.account_id,
            NEW.account_epoch,NEW.account_version,NEW.applicant_version,NEW.challenge_version,
            NEW.gate_version,NEW.payload_sealed,NEW.prepare_key,
            NEW.created_at,NEW.expires_at)
            IS DISTINCT FROM
            (OLD.id,OLD.challenge_id,OLD.actor_id,OLD.tenant_id,OLD.platform,OLD.account_id,
            OLD.account_epoch,OLD.account_version,OLD.applicant_version,OLD.challenge_version,
            OLD.gate_version,OLD.payload_sealed,OLD.prepare_key,
            OLD.created_at,OLD.expires_at)
            OR OLD.send_key IS NOT NULL AND NEW.send_key IS DISTINCT FROM OLD.send_key
            OR NEW.attempts_used<OLD.attempts_used
            OR OLD.status IN ('friends','failed') AND NEW.status<>OLD.status
            OR OLD.status='submitted' AND NEW.status NOT IN ('submitted','friends','failed')
            OR OLD.status IN ('submitting','unknown') AND NEW.status NOT IN ('submitted','friends','failed','unknown') THEN
            RAISE EXCEPTION 'immutable outbound friend scope' USING ERRCODE='23514';
        END IF;
    ELSE
        IF NOT EXISTS(SELECT 1 FROM group_binding_challenges c JOIN accounts a ON a.id=c.account_id
            JOIN group_binding_outbound_gates g ON g.platform=c.platform AND g.account_id=c.account_id
            WHERE c.id=NEW.challenge_id AND c.friend_flow='outbound' AND c.state='waiting'
            AND c.identity_sealed IS NULL AND c.expires_at>=NEW.expires_at AND c.expires_at>clock_timestamp()
            AND (c.applicant_id,c.tenant_id,c.platform,c.account_id,c.account_epoch,c.account_version,
                c.applicant_version,c.version)
                =(NEW.actor_id,NEW.tenant_id,NEW.platform,NEW.account_id,NEW.account_epoch,NEW.account_version,
                NEW.applicant_version,NEW.challenge_version)
            AND a.platform=c.platform AND a.purpose='management' AND a.epoch=c.account_epoch
            AND a.version=c.account_version AND a.auth_state='authenticated'
            AND g.state='open' AND g.version=NEW.gate_version) THEN
            RAISE EXCEPTION 'outbound friend challenge scope changed' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER group_binding_outbound_scope BEFORE INSERT OR UPDATE OR DELETE
    ON group_binding_outbound_attempts FOR EACH ROW EXECUTE FUNCTION group_binding_outbound_scope();

CREATE FUNCTION group_binding_outbound_challenge_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.friend_flow IS DISTINCT FROM OLD.friend_flow THEN
        RAISE EXCEPTION 'immutable friend onboarding flow' USING ERRCODE='23514';
    END IF;
    IF NEW.state IN ('cancelled','expired') AND NEW.state IS DISTINCT FROM OLD.state THEN
        PERFORM id FROM group_binding_outbound_attempts WHERE challenge_id=OLD.id FOR UPDATE;
        IF EXISTS(SELECT 1 FROM group_binding_outbound_attempts WHERE challenge_id=OLD.id
            AND status IN ('submitting','unknown')) THEN
            RAISE EXCEPTION 'outbound friend operation unresolved' USING ERRCODE='23514';
        END IF;
        UPDATE group_binding_outbound_attempts SET status='failed',
            result_code='group_binding_challenge_cancelled',updated_at=clock_timestamp()
            WHERE challenge_id=OLD.id AND status='prepared';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER group_binding_outbound_challenge_scope BEFORE UPDATE ON group_binding_challenges
    FOR EACH ROW EXECUTE FUNCTION group_binding_outbound_challenge_scope();

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['group_binding_outbound_gates','group_binding_outbound_attempts'] LOOP
        EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I TO wsl_platform',t);
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
        EXECUTE format('CREATE POLICY platform_only ON %I USING(current_user=''wsl_platform'') WITH CHECK(current_user=''wsl_platform'')',t);
    END LOOP;
END $$;
