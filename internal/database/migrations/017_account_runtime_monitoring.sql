CREATE INDEX binding_primary_capacity ON group_bindings(primary_account_id) WHERE active;
CREATE INDEX binding_ai_capacity ON group_bindings(ai_account_id) WHERE active AND ai_account_id IS NOT NULL;
CREATE INDEX native_monitor_source_window ON native_received_events(account_id,received_at)
    WHERE kind IN ('group','private');
CREATE INDEX native_monitor_decode_window ON native_decoded_events(account_id,decoded_at) INCLUDE(decode_ms)
    WHERE state='decoded';
CREATE INDEX native_monitor_failed_window ON native_decoded_events(account_id,received_at)
    WHERE state='failed';
ALTER TABLE inbox DROP CONSTRAINT inbox_state_check;
ALTER TABLE inbox ADD CONSTRAINT inbox_state_check
    CHECK (state IN ('pending','unresolved','processed','quarantined','shadow'));
ALTER TABLE inbox ADD COLUMN binding_epoch bigint CHECK (binding_epoch IS NULL OR binding_epoch>0);

-- Serialize additions, including direct platform writes. Existing excess rows
-- remain valid; reducing or editing them does not silently detach any group.
CREATE FUNCTION enforce_account_group_capacity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target uuid; used integer; adding boolean;
BEGIN
    PERFORM pg_advisory_xact_lock(798423);
    IF NOT NEW.active THEN RETURN NEW; END IF;
    FOR target IN SELECT unnest(ARRAY[NEW.primary_account_id,NEW.ai_account_id])
    LOOP
        IF target IS NULL THEN CONTINUE; END IF;
        adding := TG_OP='INSERT';
        IF TG_OP='UPDATE' THEN
            adding := NOT OLD.active OR
                (target IS DISTINCT FROM OLD.primary_account_id AND target IS DISTINCT FROM OLD.ai_account_id);
        END IF;
        IF adding THEN
            SELECT count(*) INTO used FROM group_bindings
                WHERE active AND group_id<>NEW.group_id
                  AND (primary_account_id=target OR ai_account_id=target);
            IF used>=30 THEN
                RAISE EXCEPTION 'account group capacity exceeded' USING ERRCODE='23514',
                    CONSTRAINT='account_group_capacity';
            END IF;
        END IF;
    END LOOP;
    RETURN NEW;
END $$;
CREATE TRIGGER account_group_capacity BEFORE INSERT OR UPDATE OF active,primary_account_id,ai_account_id
    ON group_bindings FOR EACH ROW EXECUTE FUNCTION enforce_account_group_capacity();

CREATE TABLE account_read_state (
    account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    cooldown_until timestamptz NOT NULL,
    account_epoch bigint NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE monitor_minutes (
    scope text NOT NULL CHECK (scope IN ('system','account')),
    scope_id text NOT NULL,
    account_id uuid REFERENCES accounts(id) ON DELETE CASCADE,
    minute timestamptz NOT NULL,
    workspace text NOT NULL CHECK (workspace IN ('platform','wsl','maibot')),
    sample jsonb NOT NULL CHECK (octet_length(sample::text)<=32768),
    PRIMARY KEY(scope,scope_id,minute),
    CHECK ((scope='account' AND account_id IS NOT NULL AND scope_id=account_id::text)
        OR (scope='system' AND account_id IS NULL))
);
CREATE TABLE monitor_incidents (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace text NOT NULL CHECK (workspace IN ('platform','wsl','maibot')),
    account_id uuid REFERENCES accounts(id) ON DELETE CASCADE,
    component text NOT NULL CHECK (component IN ('simon','database','receiver','decoder','http','backlog')),
    reason text NOT NULL CHECK (length(reason)<=80),
    state text NOT NULL CHECK (state IN ('opened','resolved')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT(workspace,account_id,component,reason,state,created_at)
);
CREATE INDEX monitor_incidents_time ON monitor_incidents(created_at DESC,id DESC);
CREATE INDEX monitor_incidents_account ON monitor_incidents(account_id,id DESC);
CREATE TABLE group_assignment_executions (
    actor_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request_key text NOT NULL CHECK (length(request_key) BETWEEN 8 AND 100),
    request_hash bytea NOT NULL,
    group_id uuid NOT NULL REFERENCES native_groups(id),
    result jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(actor_id,request_key)
);
DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['account_read_state','monitor_minutes','monitor_incidents','group_assignment_executions']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
        EXECUTE format('CREATE POLICY platform_only ON %I USING(current_user=''wsl_platform'') WITH CHECK(current_user=''wsl_platform'')',t);
        EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I TO wsl_platform',t);
    END LOOP;
END $$;
GRANT USAGE,SELECT ON SEQUENCE monitor_incidents_id_seq TO wsl_platform;
