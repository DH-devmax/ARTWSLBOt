-- Human demo entitlement never changes customer subscriptions or automatic work.
ALTER TABLE business_capability_gates DROP CONSTRAINT business_capability_gates_source_check;
ALTER TABLE business_capability_gates ADD CONSTRAINT business_capability_gates_source_check
    CHECK(source IN ('assistant','manual','automatic','onboarding','heatbot'));

ALTER TABLE ai_calls ADD COLUMN execution_mode text NOT NULL DEFAULT 'legacy'
    CHECK(execution_mode IN ('legacy','tenant_business','operator_demo'));
ALTER TABLE ai_calls ADD COLUMN billing_source text NOT NULL DEFAULT 'legacy'
    CHECK(billing_source IN ('legacy','tenant_wallet','platform_demo','platform_test'));
ALTER TABLE ai_calls ADD CONSTRAINT ai_call_demo_scope CHECK(
    (execution_mode<>'operator_demo' OR
        purpose='assistant' AND tenant_id IS NULL AND period_id IS NULL AND billing_source='platform_demo')
    AND (billing_source<>'platform_demo' OR execution_mode='operator_demo'));
-- Preserve the original view column order for existing consumers.
CREATE OR REPLACE VIEW ai_call_costs WITH (security_invoker=true) AS
    SELECT c.id,c.tenant_id,c.actor_id,c.group_id,c.purpose,c.period_id,c.rate_id,c.external_key,
        c.request_hash,c.reserved_micro,c.charged_micro,c.status,c.usage,c.submitted_at,c.finished_at,c.created_at,
        c.charged_micro-coalesce((SELECT sum(e.amount_micro)::bigint FROM credit_entries e
            WHERE e.call_id=c.id AND e.kind='adjustment'),0) AS effective_charged_micro,
        c.execution_mode,c.billing_source FROM ai_calls c;

CREATE FUNCTION immutable_human_mode() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.execution_mode='operator_demo' AND NOT EXISTS(
            SELECT 1 FROM users u WHERE u.id=NEW.actor_id AND u.role='superadmin' AND NOT u.disabled
        ) THEN RAISE EXCEPTION 'invalid demo actor' USING ERRCODE='23514'; END IF;
    ELSIF NEW.execution_mode IS DISTINCT FROM OLD.execution_mode THEN
        RAISE EXCEPTION 'immutable execution mode' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME='ai_calls' AND TG_OP='UPDATE' THEN
        IF NEW.billing_source IS DISTINCT FROM OLD.billing_source THEN
            RAISE EXCEPTION 'immutable billing source' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER ai_call_mode_guard BEFORE INSERT OR UPDATE ON ai_calls
    FOR EACH ROW EXECUTE FUNCTION immutable_human_mode();

ALTER TABLE assistant_plans ADD COLUMN execution_mode text NOT NULL DEFAULT 'legacy'
    CHECK(execution_mode IN ('legacy','tenant_business','operator_demo'));
ALTER TABLE assistant_action_plans ADD COLUMN execution_mode text NOT NULL DEFAULT 'legacy'
    CHECK(execution_mode IN ('legacy','tenant_business','operator_demo'));
CREATE TRIGGER assistant_config_mode_guard BEFORE INSERT OR UPDATE ON assistant_plans
    FOR EACH ROW EXECUTE FUNCTION immutable_human_mode();
CREATE TRIGGER assistant_action_mode_guard BEFORE INSERT OR UPDATE ON assistant_action_plans
    FOR EACH ROW EXECUTE FUNCTION immutable_human_mode();
ALTER TABLE assistant_turns ADD COLUMN target_sealed bytea;

CREATE TABLE manual_action_plans (
    id uuid PRIMARY KEY,
    actor_id uuid NOT NULL REFERENCES users(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    domain text NOT NULL CHECK(domain IN ('wsl','lajiaoliao')),
    account_id uuid NOT NULL REFERENCES accounts(id),
    group_id uuid NOT NULL REFERENCES native_groups(id),
    action text NOT NULL CHECK(action IN ('publish_notice','member_nickname','mute_member',
        'unmute_member','mute_group','unmute_group','kick_member','send_text','recall_message','group_info')),
    execution_mode text NOT NULL CHECK(execution_mode IN ('tenant_business','operator_demo')),
    digest text NOT NULL CHECK(length(digest)=64),
    authorization_hash text NOT NULL CHECK(length(authorization_hash)=64),
    payload_sealed bytea NOT NULL,
    account_epoch bigint NOT NULL,
    binding_epoch bigint NOT NULL,
    base_version bigint NOT NULL,
    status text NOT NULL CHECK(status IN ('pending_approval','blocked','approved','submitting',
        'succeeded','failed','unknown','cancelled','expired')),
    result_code text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1,
    idempotency_key text,
    operation_id uuid REFERENCES native_setting_operations(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    CHECK(expires_at<=created_at+interval '10 minutes'),
    UNIQUE(actor_id,idempotency_key)
);
CREATE INDEX manual_action_history ON manual_action_plans(actor_id,domain,group_id,created_at DESC);
CREATE FUNCTION manual_action_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
        JOIN accounts a ON a.id=b.primary_account_id JOIN users u ON u.id=NEW.actor_id
        WHERE b.group_id=NEW.group_id AND b.tenant_id=NEW.tenant_id AND a.id=NEW.account_id
        AND a.platform=NEW.domain AND g.platform=NEW.domain AND a.purpose='management'
        AND (u.role='superadmin' OR u.tenant_id=NEW.tenant_id)) THEN
        RAISE EXCEPTION 'manual plan scope mismatch' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER manual_action_scope BEFORE INSERT ON manual_action_plans
    FOR EACH ROW EXECUTE FUNCTION manual_action_scope();
CREATE FUNCTION manual_action_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.actor_id,NEW.tenant_id,NEW.domain,NEW.account_id,NEW.group_id,NEW.action,NEW.digest,
        NEW.authorization_hash,NEW.payload_sealed,NEW.account_epoch,NEW.binding_epoch,NEW.base_version,NEW.expires_at)
        IS DISTINCT FROM
        (OLD.actor_id,OLD.tenant_id,OLD.domain,OLD.account_id,OLD.group_id,OLD.action,OLD.digest,
        OLD.authorization_hash,OLD.payload_sealed,OLD.account_epoch,OLD.binding_epoch,OLD.base_version,OLD.expires_at) THEN
        RAISE EXCEPTION 'immutable manual plan' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER manual_action_immutable BEFORE UPDATE ON manual_action_plans
    FOR EACH ROW EXECUTE FUNCTION manual_action_immutable();
CREATE TRIGGER manual_action_mode_guard BEFORE INSERT OR UPDATE ON manual_action_plans
    FOR EACH ROW EXECUTE FUNCTION immutable_human_mode();

CREATE TABLE manual_message_attempts (LIKE assistant_message_attempts INCLUDING ALL);
ALTER TABLE manual_message_attempts ADD FOREIGN KEY(operation_id) REFERENCES native_setting_operations(id) ON DELETE CASCADE;
CREATE TRIGGER manual_attempt_immutable BEFORE UPDATE ON manual_message_attempts
    FOR EACH ROW EXECUTE FUNCTION assistant_attempt_immutable();
DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['manual_action_plans','manual_message_attempts'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
        EXECUTE format('CREATE POLICY platform_only ON %I USING(current_user=''wsl_platform'') WITH CHECK(current_user=''wsl_platform'')',t);
        EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I TO wsl_platform',t);
    END LOOP;
END $$;
