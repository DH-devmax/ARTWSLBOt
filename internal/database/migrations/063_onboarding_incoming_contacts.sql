-- Contact approval and website/native identity are separate evidence paths.
ALTER TABLE group_binding_challenges DROP CONSTRAINT group_binding_challenges_friend_flow_check;
ALTER TABLE group_binding_challenges ADD CONSTRAINT group_binding_challenges_friend_flow_check
    CHECK(friend_flow IN ('legacy','outbound','incoming_contact'));
ALTER TABLE group_binding_challenges ALTER COLUMN friend_flow SET DEFAULT 'incoming_contact';

-- Existing expiry locks the challenge first and terminates its prepared
-- attempt atomically. Only submitted/unknown work must block that cleanup.
CREATE OR REPLACE FUNCTION group_binding_friend_pending(challenge uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS(SELECT 1 FROM group_binding_friend_attempts a
        JOIN group_binding_challenges c ON c.id=a.challenge_id
        WHERE a.challenge_id=challenge AND (a.status IN ('submitting','unknown')
            OR a.status='prepared' AND (c.friend_flow<>'incoming_contact' OR a.expires_at>clock_timestamp())))
        OR EXISTS(SELECT 1 FROM group_binding_outbound_attempts WHERE challenge_id=challenge
            AND status IN ('submitting','unknown'))
$$;

CREATE OR REPLACE FUNCTION group_binding_friend_scope() RETURNS trigger LANGUAGE plpgsql AS $$
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
    IF NOT EXISTS(SELECT 1 FROM group_binding_challenges c JOIN accounts a ON a.id=c.account_id
        WHERE c.id=NEW.challenge_id AND c.state='waiting' AND c.identity_sealed IS NULL
        AND c.expires_at>clock_timestamp()
        AND (c.friend_flow='incoming_contact' AND TG_TABLE_NAME='group_binding_friend_attempts'
            OR c.friend_flow='legacy' AND c.friend_token_hash IS NOT NULL AND c.friend_text_sealed IS NOT NULL)
        AND a.platform=c.platform AND a.purpose='management' AND a.auth_state='authenticated'
        AND a.epoch=c.account_epoch AND a.version=c.account_version
        AND (c.applicant_id,c.tenant_id,c.platform,c.account_id,c.account_epoch,c.account_version,c.applicant_version,c.version)
            =(NEW.actor_id,NEW.tenant_id,NEW.platform,NEW.account_id,NEW.account_epoch,NEW.account_version,NEW.applicant_version,NEW.challenge_version)) THEN
        RAISE EXCEPTION 'onboarding friend challenge scope' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME='group_binding_friend_attempts' THEN
        IF NEW.status<>'prepared' OR NEW.attempts_used<>0 OR NOT EXISTS(
            SELECT 1 FROM group_binding_challenges c WHERE c.id=NEW.challenge_id AND c.expires_at>=NEW.expires_at)
            OR NOT EXISTS(
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

CREATE FUNCTION group_binding_contact_identity_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.friend_flow='incoming_contact' AND NEW.identity_friend_proof_id IS NOT NULL THEN
        RAISE EXCEPTION 'contact approval is not identity evidence' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER group_binding_contact_identity_scope BEFORE INSERT OR UPDATE ON group_binding_challenges
    FOR EACH ROW EXECUTE FUNCTION group_binding_contact_identity_scope();

CREATE INDEX group_binding_friend_actor_rate ON group_binding_friend_attempts(actor_id,updated_at)
    WHERE attempts_used=1;
CREATE INDEX group_binding_friend_account_rate ON group_binding_friend_attempts(account_id,updated_at)
    WHERE attempts_used=1;
