-- Background attribution is separate from human operator_demo/platform_demo.
-- No subscription, wallet, business switch or source gate is enabled here.
ALTER TABLE ai_calls DROP CONSTRAINT ai_calls_execution_mode_check;
ALTER TABLE ai_calls ADD CONSTRAINT ai_calls_execution_mode_check
    CHECK(execution_mode IN ('legacy','tenant_business','operator_demo','workspace_demo'));
ALTER TABLE ai_calls DROP CONSTRAINT ai_calls_billing_source_check;
ALTER TABLE ai_calls ADD CONSTRAINT ai_calls_billing_source_check
    CHECK(billing_source IN ('legacy','tenant_wallet','platform_demo','platform_test','platform_workspace'));
-- Keep the task identifier after account/task retention cleanup. Its scope is
-- validated against the durable task on insert and cannot subsequently change.
ALTER TABLE ai_calls ADD COLUMN service_task_id uuid;
ALTER TABLE ai_calls ADD CONSTRAINT ai_call_workspace_scope CHECK(
    (execution_mode<>'workspace_demo' OR purpose='heatbot' AND tenant_id IS NOT NULL
        AND group_id IS NOT NULL AND period_id IS NULL AND service_task_id IS NOT NULL
        AND billing_source='platform_workspace')
    AND (billing_source<>'platform_workspace' OR execution_mode='workspace_demo')
    AND (service_task_id IS NULL OR purpose='heatbot' AND tenant_id IS NOT NULL AND group_id IS NOT NULL));

CREATE FUNCTION ai_call_workspace_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' THEN
        IF (NEW.id,NEW.tenant_id,NEW.actor_id,NEW.group_id,NEW.purpose,NEW.period_id,NEW.rate_id,
            NEW.external_key,NEW.request_hash,NEW.reserved_micro,NEW.created_at,NEW.service_task_id)
            IS DISTINCT FROM
            (OLD.id,OLD.tenant_id,OLD.actor_id,OLD.group_id,OLD.purpose,OLD.period_id,OLD.rate_id,
            OLD.external_key,OLD.request_hash,OLD.reserved_micro,OLD.created_at,OLD.service_task_id) THEN
            RAISE EXCEPTION 'immutable AI call scope' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.execution_mode='workspace_demo' AND NOT workspace_is_demo(NEW.tenant_id,NEW.actor_id) THEN
        RAISE EXCEPTION 'invalid workspace service owner' USING ERRCODE='23514';
    END IF;
    IF NEW.purpose='heatbot' AND NEW.execution_mode<>'workspace_demo' AND EXISTS(
        SELECT 1 FROM tenants WHERE id=NEW.tenant_id AND space_kind='operator_demo'
    ) THEN
        RAISE EXCEPTION 'demo background requires workspace billing' USING ERRCODE='23514';
    END IF;
    IF NEW.service_task_id IS NOT NULL AND NOT EXISTS(
        SELECT 1 FROM heat_chat_jobs j JOIN heat_gateway_grants h ON h.chat_job_id=j.id
        JOIN group_bindings b ON b.group_id=j.group_id AND b.tenant_id=j.tenant_id
        JOIN native_groups g ON g.id=b.group_id
        JOIN accounts a ON a.id=j.account_id
        WHERE j.id=NEW.service_task_id AND j.tenant_id=NEW.tenant_id AND j.actor_id=NEW.actor_id
        AND j.group_id=NEW.group_id AND j.status='generating' AND j.expires_at>clock_timestamp()
        AND NEW.external_key='heat-job:'||j.id::text
        AND h.tenant_id=j.tenant_id AND h.actor_id=j.actor_id AND h.group_id=j.group_id
        AND h.account_id=j.account_id AND h.used_at IS NOT NULL AND h.generation_deadline>clock_timestamp()
        AND b.active AND b.ai_requested AND b.ai_enabled AND b.ai_account_id=a.id
        AND b.version=h.binding_version AND b.epoch=h.binding_epoch AND a.epoch=h.account_epoch
        AND a.platform=j.platform AND g.platform=j.platform AND a.purpose='chat'
    ) THEN
        RAISE EXCEPTION 'invalid workspace service task' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER ai_call_workspace_guard BEFORE INSERT OR UPDATE ON ai_calls
    FOR EACH ROW EXECUTE FUNCTION ai_call_workspace_scope();

-- Keep every existing view column in its original position.
CREATE OR REPLACE VIEW ai_call_costs WITH (security_invoker=true) AS
    SELECT c.id,c.tenant_id,c.actor_id,c.group_id,c.purpose,c.period_id,c.rate_id,c.external_key,
        c.request_hash,c.reserved_micro,c.charged_micro,c.status,c.usage,c.submitted_at,c.finished_at,c.created_at,
        c.charged_micro-coalesce((SELECT sum(e.amount_micro)::bigint FROM credit_entries e
            WHERE e.call_id=c.id AND e.kind='adjustment'),0) AS effective_charged_micro,
        c.execution_mode,c.billing_source,c.service_task_id FROM ai_calls c;

CREATE OR REPLACE FUNCTION business_rule_consistent_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM group_bindings b
        JOIN native_groups g ON g.id=b.group_id JOIN accounts a ON a.id=b.primary_account_id
        WHERE b.group_id=NEW.group_id AND b.tenant_id=NEW.tenant_id AND a.id=NEW.account_id
        AND a.purpose='management' AND a.platform=NEW.platform AND g.platform=NEW.platform) THEN
        RAISE EXCEPTION 'business rule scope mismatch' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME='business_rule_jobs' THEN
        IF NOT EXISTS(
            SELECT 1 FROM users u JOIN tenants t ON t.owner_id=u.id
            WHERE u.id=NEW.actor_id AND t.id=NEW.tenant_id AND NOT u.disabled
            AND (workspace_is_demo(t.id,u.id) OR t.space_kind='tenant_business'
                AND u.role='user' AND u.tenant_id=t.id AND u.tenant_role='owner')
        ) THEN
            RAISE EXCEPTION 'business rule actor mismatch' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE FUNCTION workspace_heat_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS(SELECT 1 FROM tenants WHERE id=NEW.tenant_id AND space_kind='operator_demo') THEN
        IF NOT workspace_is_demo(NEW.tenant_id,NEW.actor_id) OR NOT EXISTS(
            SELECT 1 FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
            JOIN group_business_runtime r ON r.group_id=b.group_id AND r.tenant_id=b.tenant_id
            JOIN accounts a ON a.id=b.ai_account_id
            WHERE b.group_id=NEW.group_id AND b.tenant_id=NEW.tenant_id AND b.active
            AND b.ai_requested AND b.ai_enabled AND r.enabled
            AND a.id=NEW.account_id AND a.purpose='chat' AND a.platform=g.platform
        ) THEN
            RAISE EXCEPTION 'invalid demo heat scope' USING ERRCODE='23514';
        END IF;
        IF TG_TABLE_NAME='heat_gateway_grants' THEN
            IF NEW.chat_job_id IS NULL OR NOT EXISTS(
                SELECT 1 FROM heat_chat_jobs j WHERE j.id=NEW.chat_job_id AND j.status='generating'
                AND j.tenant_id=NEW.tenant_id AND j.actor_id=NEW.actor_id
                AND j.group_id=NEW.group_id AND j.account_id=NEW.account_id
                AND j.expires_at>clock_timestamp()
            ) THEN
                RAISE EXCEPTION 'demo heat requires durable task' USING ERRCODE='23514';
            END IF;
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER workspace_heat_job_scope BEFORE INSERT ON heat_chat_jobs
    FOR EACH ROW EXECUTE FUNCTION workspace_heat_scope();
CREATE TRIGGER workspace_heat_grant_scope BEFORE INSERT ON heat_gateway_grants
    FOR EACH ROW EXECUTE FUNCTION workspace_heat_scope();
