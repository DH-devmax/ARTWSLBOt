-- Exact contact lookup is read-only. Admission is pinned only when a person
-- submits the one durable friend request, before any native write can occur.
ALTER TABLE group_binding_outbound_attempts
    DROP CONSTRAINT group_binding_outbound_attempts_gate_version_check;
ALTER TABLE group_binding_outbound_attempts
    ADD CONSTRAINT group_binding_outbound_attempts_gate_version_check CHECK(gate_version>=0),
    ADD CONSTRAINT group_binding_outbound_submission_admission
        CHECK(attempts_used=0 OR gate_version>0);

CREATE OR REPLACE FUNCTION group_binding_outbound_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    admission_pinned boolean;
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'immutable outbound friend history' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' THEN
        admission_pinned := OLD.gate_version=0 AND OLD.status='prepared'
            AND OLD.attempts_used=0 AND OLD.send_key IS NULL
            AND NEW.status='submitting' AND NEW.attempts_used=1
            AND NEW.send_key IS NOT NULL AND NEW.gate_version>0;
        IF (NEW.id,NEW.challenge_id,NEW.actor_id,NEW.tenant_id,NEW.platform,NEW.account_id,
            NEW.account_epoch,NEW.account_version,NEW.applicant_version,NEW.challenge_version,
            NEW.payload_sealed,NEW.prepare_key,NEW.created_at,NEW.expires_at)
            IS DISTINCT FROM
            (OLD.id,OLD.challenge_id,OLD.actor_id,OLD.tenant_id,OLD.platform,OLD.account_id,
            OLD.account_epoch,OLD.account_version,OLD.applicant_version,OLD.challenge_version,
            OLD.payload_sealed,OLD.prepare_key,OLD.created_at,OLD.expires_at)
            OR NEW.gate_version IS DISTINCT FROM OLD.gate_version AND NOT admission_pinned
            OR OLD.send_key IS NOT NULL AND NEW.send_key IS DISTINCT FROM OLD.send_key
            OR NEW.attempts_used<OLD.attempts_used
            OR OLD.status IN ('friends','failed') AND NEW.status<>OLD.status
            OR OLD.status='submitted' AND NEW.status NOT IN ('submitted','friends','failed')
            OR OLD.status IN ('submitting','unknown') AND NEW.status NOT IN ('submitted','friends','failed','unknown') THEN
            RAISE EXCEPTION 'immutable outbound friend scope' USING ERRCODE='23514';
        END IF;
        IF (admission_pinned OR OLD.status='prepared' AND NEW.status='submitting')
            AND NOT EXISTS(SELECT 1 FROM group_binding_outbound_gates g
                WHERE g.platform=NEW.platform AND g.account_id=NEW.account_id
                AND g.state='open' AND g.version=NEW.gate_version) THEN
            RAISE EXCEPTION 'outbound friend submission admission changed' USING ERRCODE='23514';
        END IF;
    ELSE
        IF NOT EXISTS(SELECT 1 FROM group_binding_challenges c JOIN accounts a ON a.id=c.account_id
            WHERE c.id=NEW.challenge_id AND c.friend_flow='outbound' AND c.state='waiting'
            AND c.identity_sealed IS NULL AND c.expires_at>=NEW.expires_at AND c.expires_at>clock_timestamp()
            AND (c.applicant_id,c.tenant_id,c.platform,c.account_id,c.account_epoch,c.account_version,
                c.applicant_version,c.version)
                =(NEW.actor_id,NEW.tenant_id,NEW.platform,NEW.account_id,NEW.account_epoch,NEW.account_version,
                NEW.applicant_version,NEW.challenge_version)
            AND a.platform=c.platform AND a.purpose='management' AND a.epoch=c.account_epoch
            AND a.version=c.account_version AND a.auth_state='authenticated'
            AND (NEW.gate_version=0 AND NEW.attempts_used=0 AND NEW.send_key IS NULL
                    AND NEW.status IN ('prepared','friends')
                OR NEW.gate_version>0 AND EXISTS(SELECT 1 FROM group_binding_outbound_gates g
                    WHERE g.platform=c.platform AND g.account_id=c.account_id
                    AND g.state='open' AND g.version=NEW.gate_version))) THEN
            RAISE EXCEPTION 'outbound friend challenge scope changed' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
