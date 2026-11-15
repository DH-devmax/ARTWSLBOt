-- Retained outgoing contact Tools use a separate persisted superadmin window.
-- Existing outgoing history and the customer's incoming-contact flow keep
-- their immutable scopes; a controlled contact never becomes an identity proof.
ALTER TABLE group_binding_outbound_attempts
    ADD COLUMN test_window_id uuid REFERENCES test_windows(id);

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
            NEW.payload_sealed,NEW.prepare_key,NEW.created_at,NEW.expires_at,NEW.test_window_id)
            IS DISTINCT FROM
            (OLD.id,OLD.challenge_id,OLD.actor_id,OLD.tenant_id,OLD.platform,OLD.account_id,
            OLD.account_epoch,OLD.account_version,OLD.applicant_version,OLD.challenge_version,
            OLD.payload_sealed,OLD.prepare_key,OLD.created_at,OLD.expires_at,OLD.test_window_id)
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
            WHERE c.id=NEW.challenge_id AND c.state='waiting'
            AND (c.friend_flow='outbound' AND NEW.test_window_id IS NULL
                OR c.friend_flow='incoming_contact' AND c.applicant_mode='operator_demo'
                    AND NEW.test_window_id IS NOT NULL)
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
    IF NEW.test_window_id IS NOT NULL AND (TG_OP='INSERT' OR admission_pinned) THEN
        IF NOT EXISTS(SELECT 1 FROM test_windows w JOIN users u ON u.id=w.actor_id
            JOIN native_groups g ON g.id=w.group_id JOIN group_bindings b ON b.group_id=g.id
            JOIN accounts a ON a.id=NEW.account_id
            WHERE w.id=NEW.test_window_id AND w.actor_id=NEW.actor_id
            AND u.role='superadmin' AND NOT u.disabled AND w.admission_mode='native_debug'
            AND w.actions=ARRAY['onboarding_friend_search','onboarding_friend_request']::text[]
            AND cardinality(w.target_ids)=4
            AND w.target_ids[1]='onboarding_challenge:'||NEW.challenge_id::text||':'||NEW.challenge_version::text
            AND w.target_ids[2] LIKE 'onboarding_account:%' AND w.target_ids[3] LIKE 'onboarding_target:%'
            AND w.target_ids[4]='onboarding_binding:'||b.epoch::text
            AND w.account_ids=ARRAY[NEW.account_id] AND w.account_epochs->>NEW.account_id::text=NEW.account_epoch::text
            AND NOT w.cancelled AND w.expires_at=NEW.expires_at AND w.expires_at>clock_timestamp()+interval '4 seconds'
            AND w.used<=w.budget AND (TG_OP='INSERT' AND w.used<w.budget OR TG_OP='UPDATE' AND w.used>0)
            AND g.isolated_test AND g.platform=NEW.platform AND b.active
            AND b.tenant_id=NEW.tenant_id AND b.primary_account_id=NEW.account_id
            AND a.testing_enabled AND a.connection_enabled AND a.auth_state='authenticated'
            AND a.credential_sealed IS NOT NULL AND a.platform=NEW.platform AND a.workspace=NEW.platform
            AND a.purpose='management' AND a.epoch=NEW.account_epoch AND a.version=NEW.account_version) THEN
            RAISE EXCEPTION 'outbound friend controlled window changed' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
