-- Keep the existing binding unchanged while an explicit applicant test proves
-- ownership. Customer ownership still requires authenticated ingress and review.
CREATE OR REPLACE FUNCTION group_binding_outbound_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    admission_pinned boolean := false;
    first_submission boolean := false;
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'immutable outbound friend history' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' THEN
        first_submission := OLD.status='prepared' AND OLD.attempts_used=0
            AND OLD.send_key IS NULL AND NEW.status='submitting'
            AND NEW.attempts_used=1 AND NEW.send_key IS NOT NULL;
        admission_pinned := first_submission AND OLD.gate_version=0 AND NEW.gate_version>0;
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
        IF first_submission AND NOT EXISTS(SELECT 1 FROM group_binding_outbound_gates g
            WHERE g.platform=NEW.platform AND g.account_id=NEW.account_id
            AND g.state='open' AND g.version=NEW.gate_version) THEN
            RAISE EXCEPTION 'outbound friend submission admission changed' USING ERRCODE='23514';
        END IF;
    END IF;
    IF TG_OP='INSERT' OR first_submission THEN
        IF NOT EXISTS(
            SELECT 1 FROM group_binding_challenges c
            JOIN accounts receiver ON receiver.id=c.account_id
            JOIN accounts sender ON sender.id=NEW.account_id
            WHERE c.id=NEW.challenge_id AND c.state='waiting'
            AND (c.friend_flow='outbound' AND NEW.test_window_id IS NULL
                OR c.friend_flow='incoming_contact' AND c.applicant_mode='operator_demo'
                    AND NEW.test_window_id IS NOT NULL)
            AND c.identity_sealed IS NULL AND c.expires_at>=NEW.expires_at
            AND c.expires_at>clock_timestamp()
            AND (c.applicant_id,c.tenant_id,c.platform,c.applicant_version,c.version)
                =(NEW.actor_id,NEW.tenant_id,NEW.platform,NEW.applicant_version,NEW.challenge_version)
            AND receiver.platform=c.platform AND receiver.purpose='management'
            AND receiver.epoch=c.account_epoch AND receiver.version=c.account_version
            AND receiver.auth_state='authenticated' AND receiver.credential_sealed IS NOT NULL
            AND sender.platform=c.platform AND sender.workspace=c.platform AND sender.purpose='management'
            AND sender.epoch=NEW.account_epoch AND sender.version=NEW.account_version
            AND sender.auth_state='authenticated' AND sender.credential_sealed IS NOT NULL
            AND (
                (c.account_id,c.account_epoch,c.account_version)
                    =(NEW.account_id,NEW.account_epoch,NEW.account_version)
                OR c.friend_flow='incoming_contact' AND c.applicant_mode='operator_demo'
                    AND EXISTS(SELECT 1 FROM onboarding_test_scopes s JOIN test_windows w ON w.id=NEW.test_window_id
                        WHERE s.id=c.controlled_scope_id AND onboarding_test_scope_active(s.id)
                        AND s.actor_id=c.applicant_id AND s.tenant_id=c.tenant_id AND s.platform=c.platform
                        AND (s.account_id,s.account_epoch,s.account_version)
                            =(c.account_id,c.account_epoch,c.account_version)
                        AND (s.claimant_account_id,s.claimant_epoch,s.claimant_version)
                            =(NEW.account_id,NEW.account_epoch,NEW.account_version)
                        AND s.group_id=c.group_id AND s.group_id=w.group_id
                        AND s.expires_at>=NEW.expires_at AND receiver.connection_enabled
                        AND w.budget=1 AND w.target_ids[3]='onboarding_target:'||receiver.native_uid)
            )
            AND (TG_OP='INSERT' AND NEW.gate_version=0 AND NEW.attempts_used=0
                    AND NEW.send_key IS NULL AND NEW.status IN ('prepared','friends')
                OR NEW.gate_version>0 AND EXISTS(SELECT 1 FROM group_binding_outbound_gates g
                    WHERE g.platform=NEW.platform AND g.account_id=NEW.account_id
                    AND g.state='open' AND g.version=NEW.gate_version))
        ) THEN
            RAISE EXCEPTION 'outbound friend challenge scope changed' USING ERRCODE='23514';
        END IF;
        IF NEW.test_window_id IS NOT NULL AND NOT EXISTS(
            SELECT 1 FROM test_windows w JOIN users u ON u.id=w.actor_id
            JOIN native_groups g ON g.id=w.group_id JOIN group_bindings b ON b.group_id=g.id
            JOIN accounts a ON a.id=NEW.account_id JOIN group_binding_challenges c ON c.id=NEW.challenge_id
            WHERE w.id=NEW.test_window_id AND w.actor_id=NEW.actor_id
            AND u.role='superadmin' AND NOT u.disabled AND w.admission_mode='native_debug'
            AND w.actions=ARRAY['onboarding_friend_search','onboarding_friend_request']::text[]
            AND cardinality(w.target_ids)=4
            AND w.target_ids[1]='onboarding_challenge:'||NEW.challenge_id::text||':'||NEW.challenge_version::text
            AND w.target_ids[2] LIKE 'onboarding_account:%' AND w.target_ids[3] LIKE 'onboarding_target:%'
            AND w.account_ids=ARRAY[NEW.account_id]
            AND w.account_epochs->>NEW.account_id::text=NEW.account_epoch::text
            AND NOT w.cancelled AND w.expires_at=NEW.expires_at
            AND w.expires_at>clock_timestamp()+interval '4 seconds'
            AND w.used<=w.budget AND (TG_OP='INSERT' AND w.used<w.budget OR TG_OP='UPDATE' AND w.used>0)
            AND g.isolated_test AND g.platform=NEW.platform AND b.active
            AND (
                b.tenant_id=NEW.tenant_id AND b.primary_account_id=NEW.account_id AND c.account_id=NEW.account_id
                    AND w.target_ids[4]='onboarding_binding:'||b.epoch::text
                OR EXISTS(SELECT 1 FROM onboarding_test_scopes s JOIN accounts receiver ON receiver.id=s.account_id
                    WHERE s.id=c.controlled_scope_id AND onboarding_test_scope_active(s.id)
                    AND s.actor_id=NEW.actor_id AND s.tenant_id=NEW.tenant_id AND s.platform=NEW.platform
                    AND s.group_id=g.id
                    AND s.account_id=c.account_id AND s.account_epoch=c.account_epoch
                    AND s.account_version=c.account_version AND receiver.epoch=s.account_epoch
                    AND receiver.version=s.account_version AND receiver.connection_enabled
                    AND s.claimant_account_id=NEW.account_id AND s.claimant_epoch=NEW.account_epoch
                    AND s.claimant_version=NEW.account_version AND w.budget=1
                    AND w.target_ids[3]='onboarding_target:'||receiver.native_uid
                    AND w.target_ids[4]='onboarding_binding:'||b.epoch::text||':'||b.version::text
                        ||':'||b.tenant_id::text||':'||b.primary_account_id::text
                    AND (b.tenant_id=s.tenant_id AND b.primary_account_id=s.account_id
                            AND EXISTS(SELECT 1 FROM group_binding_ownership o WHERE o.group_id=g.id
                                AND o.tenant_id=s.tenant_id AND o.platform=s.platform
                                AND o.proof_kind='controlled_test' AND o.state='paused')
                        OR b.primary_account_id IN(s.account_id,s.claimant_account_id)
                            AND NOT EXISTS(SELECT 1 FROM group_binding_ownership o WHERE o.group_id=g.id))
                    AND s.expires_at>=NEW.expires_at
                    AND workspace_is_demo(NEW.tenant_id,NEW.actor_id))
            )
            AND a.testing_enabled AND a.connection_enabled AND a.auth_state='authenticated'
            AND a.credential_sealed IS NOT NULL AND a.platform=NEW.platform AND a.workspace=NEW.platform
            AND a.purpose='management' AND a.epoch=NEW.account_epoch AND a.version=NEW.account_version
        ) THEN
            RAISE EXCEPTION 'outbound friend controlled window changed' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
