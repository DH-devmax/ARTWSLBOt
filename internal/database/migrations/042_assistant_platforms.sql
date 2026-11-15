-- Existing rows keep their WSL identity and sealed formats.
DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['assistant_sessions','assistant_turns','assistant_events',
        'assistant_usage','assistant_plans','assistant_plan_executions']
    LOOP
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I',t,t||'_domain_check');
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK(domain IN (''wsl'',''lajiaoliao''))',
            t,t||'_domain_check');
    END LOOP;
END $$;

CREATE FUNCTION assistant_consistent_scope() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
DECLARE parent record;
BEGIN
    IF TG_TABLE_NAME='assistant_sessions' THEN
        IF TG_OP='UPDATE' AND
            (NEW.actor_id,NEW.tenant_id,NEW.domain,NEW.scope_kind,NEW.account_id,NEW.group_id)
            IS DISTINCT FROM
            (OLD.actor_id,OLD.tenant_id,OLD.domain,OLD.scope_kind,OLD.account_id,OLD.group_id) THEN
            RAISE EXCEPTION 'immutable assistant scope' USING ERRCODE='23514';
        END IF;
        IF NEW.account_id IS NOT NULL AND NOT EXISTS(
            SELECT 1 FROM public.accounts a WHERE a.id=NEW.account_id
            AND a.workspace=NEW.domain AND a.platform=NEW.domain AND a.purpose='management') THEN
            RAISE EXCEPTION 'assistant platform mismatch' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_TABLE_NAME IN ('assistant_turns','assistant_plans','assistant_action_plans') THEN
        SELECT actor_id,tenant_id,domain,account_id,group_id INTO parent
            FROM public.assistant_sessions WHERE id=NEW.session_id;
    ELSIF TG_TABLE_NAME IN ('assistant_events','assistant_usage') THEN
        SELECT actor_id,tenant_id,domain INTO parent
            FROM public.assistant_turns WHERE id=NEW.turn_id;
    ELSE
        SELECT actor_id,tenant_id,domain INTO parent
            FROM public.assistant_plans WHERE id=NEW.plan_id;
    END IF;
    IF NOT FOUND OR (NEW.actor_id,NEW.domain) IS DISTINCT FROM (parent.actor_id,parent.domain)
        OR (parent.tenant_id IS NOT NULL AND NEW.tenant_id IS DISTINCT FROM parent.tenant_id) THEN
        RAISE EXCEPTION 'assistant parent scope mismatch' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME IN ('assistant_plans','assistant_action_plans') THEN
        IF (NEW.account_id,NEW.group_id) IS DISTINCT FROM (parent.account_id,parent.group_id) THEN
            RAISE EXCEPTION 'assistant group mismatch' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['assistant_sessions','assistant_turns','assistant_events',
        'assistant_usage','assistant_plans','assistant_plan_executions']
    LOOP
        EXECUTE format('CREATE TRIGGER assistant_scope_guard BEFORE INSERT OR UPDATE ON %I
            FOR EACH ROW EXECUTE FUNCTION assistant_consistent_scope()',t);
    END LOOP;
END $$;
CREATE INDEX assistant_scope_history ON assistant_sessions(actor_id,domain,group_id,created_at DESC);
