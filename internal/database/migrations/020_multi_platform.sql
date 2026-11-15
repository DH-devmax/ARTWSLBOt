ALTER TABLE accounts ADD COLUMN platform text NOT NULL DEFAULT 'wsl'
    CHECK (platform IN ('wsl','lajiaoliao'));
ALTER TABLE accounts ADD COLUMN purpose text NOT NULL DEFAULT 'management'
    CHECK (purpose IN ('management','chat'));
UPDATE accounts SET purpose='chat' WHERE workspace='maibot';
ALTER TABLE accounts DROP CONSTRAINT accounts_workspace_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_workspace_check
    CHECK (workspace IN ('wsl','lajiaoliao','maibot'));
ALTER TABLE accounts DROP CONSTRAINT accounts_native_uid_key;
CREATE UNIQUE INDEX accounts_platform_identity ON accounts(platform,native_uid);
GRANT SELECT(platform,purpose) ON accounts TO wsl_tenant;

ALTER TABLE native_groups ADD COLUMN platform text NOT NULL DEFAULT 'wsl'
    CHECK (platform IN ('wsl','lajiaoliao'));
ALTER TABLE native_groups DROP CONSTRAINT native_groups_native_id_key;
CREATE UNIQUE INDEX native_groups_platform_identity ON native_groups(platform,native_id);

ALTER TABLE plans DROP CONSTRAINT plans_group_limit_check;
ALTER TABLE plans DROP CONSTRAINT plans_ai_group_limit_check;
ALTER TABLE plans ADD CONSTRAINT plans_group_limit_check CHECK (group_limit BETWEEN 0 AND 300);
ALTER TABLE plans ADD CONSTRAINT plans_ai_group_limit_check
    CHECK (ai_group_limit BETWEEN 0 AND 300 AND ai_group_limit<=group_limit);
ALTER TABLE plans ADD COLUMN allowed_platforms text[] NOT NULL DEFAULT ARRAY['wsl']::text[]
    CHECK (cardinality(allowed_platforms) BETWEEN 1 AND 2
        AND allowed_platforms <@ ARRAY['wsl','lajiaoliao']::text[]);

CREATE FUNCTION enforce_native_account_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE used integer;
BEGIN
    IF TG_OP='UPDATE' THEN
        IF NEW.platform<>OLD.platform OR NEW.purpose<>OLD.purpose OR NEW.workspace<>OLD.workspace THEN
            RAISE EXCEPTION 'account scope is immutable' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    -- Preserve legacy insert callers while giving each account an explicit scope.
    IF NEW.workspace='maibot' THEN NEW.purpose:='chat'; END IF;
    IF NEW.workspace='lajiaoliao' THEN NEW.platform:='lajiaoliao'; END IF;
    NEW.workspace:=CASE WHEN NEW.purpose='chat' THEN 'maibot' ELSE NEW.platform END;
    PERFORM pg_advisory_xact_lock(798422);
    SELECT count(*) INTO used FROM accounts WHERE purpose=NEW.purpose;
    IF used>=10 THEN
        RAISE EXCEPTION 'account pool capacity exceeded' USING ERRCODE='23514',
            CONSTRAINT='account_pool_capacity';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER native_account_scope BEFORE INSERT OR UPDATE OF platform,purpose,workspace ON accounts
    FOR EACH ROW EXECUTE FUNCTION enforce_native_account_scope();

CREATE OR REPLACE FUNCTION enforce_account_workspace() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE native_platform text;
BEGIN
    SELECT platform INTO STRICT native_platform FROM native_groups WHERE id=NEW.group_id;
    IF NEW.primary_account_id IS NOT NULL AND NOT EXISTS(
        SELECT 1 FROM accounts WHERE id=NEW.primary_account_id
        AND purpose='management' AND platform=native_platform) THEN
        RAISE EXCEPTION 'management account platform/purpose mismatch' USING ERRCODE='23514';
    END IF;
    IF NEW.ai_account_id IS NOT NULL AND NOT EXISTS(
        SELECT 1 FROM accounts WHERE id=NEW.ai_account_id
        AND purpose='chat' AND platform=native_platform) THEN
        RAISE EXCEPTION 'chat account platform/purpose mismatch' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;

CREATE FUNCTION enforce_membership_platform() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM accounts a JOIN native_groups g ON g.platform=a.platform
        WHERE a.id=NEW.account_id AND g.id=NEW.group_id) THEN
        RAISE EXCEPTION 'membership platform mismatch' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER membership_platform BEFORE INSERT OR UPDATE OF account_id,group_id ON account_groups
    FOR EACH ROW EXECUTE FUNCTION enforce_membership_platform();

CREATE FUNCTION enforce_group_platform_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.platform<>OLD.platform THEN
        RAISE EXCEPTION 'group platform is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER group_platform_immutable BEFORE UPDATE OF platform ON native_groups
    FOR EACH ROW EXECUTE FUNCTION enforce_group_platform_immutable();

CREATE FUNCTION enforce_group_pool_capacity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE used integer; adding boolean; old_chat boolean:=false; new_chat boolean;
BEGIN
    PERFORM pg_advisory_xact_lock(798423);
    IF NOT NEW.active THEN RETURN NEW; END IF;
    adding:=TG_OP='INSERT';
    IF TG_OP='UPDATE' THEN
        adding:=NOT OLD.active;
        old_chat:=OLD.active AND (OLD.ai_account_id IS NOT NULL OR OLD.ai_requested OR OLD.ai_enabled);
    END IF;
    IF adding THEN
        SELECT count(*) INTO used FROM group_bindings WHERE active AND group_id<>NEW.group_id;
        IF used>=300 THEN
            RAISE EXCEPTION 'management group pool full' USING ERRCODE='23514',CONSTRAINT='group_pool_capacity';
        END IF;
    END IF;
    new_chat:=NEW.ai_account_id IS NOT NULL OR NEW.ai_requested OR NEW.ai_enabled;
    IF new_chat AND NOT old_chat THEN
        SELECT count(*) INTO used FROM group_bindings WHERE active AND group_id<>NEW.group_id
            AND (ai_account_id IS NOT NULL OR ai_requested OR ai_enabled);
        IF used>=300 THEN
            RAISE EXCEPTION 'chat group pool full' USING ERRCODE='23514',CONSTRAINT='group_pool_capacity';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER group_pool_capacity BEFORE INSERT OR UPDATE OF active,ai_account_id,ai_requested,ai_enabled
    ON group_bindings FOR EACH ROW EXECUTE FUNCTION enforce_group_pool_capacity();

ALTER POLICY visible_accounts ON accounts USING (
    current_user='wsl_platform' OR (purpose='management' AND EXISTS (
        SELECT 1 FROM group_bindings b WHERE b.primary_account_id=accounts.id
    ))
);
ALTER TABLE monitor_minutes DROP CONSTRAINT monitor_minutes_workspace_check;
ALTER TABLE monitor_minutes ADD CHECK (workspace IN ('platform','wsl','lajiaoliao','maibot'));
ALTER TABLE monitor_incidents DROP CONSTRAINT monitor_incidents_workspace_check;
ALTER TABLE monitor_incidents ADD CHECK (workspace IN ('platform','wsl','lajiaoliao','maibot'));

CREATE TABLE group_chat_preferences (
    group_id uuid PRIMARY KEY REFERENCES native_groups(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    persona_id uuid REFERENCES accounts(id) ON DELETE SET NULL,
    activity integer NOT NULL DEFAULT 20 CHECK (activity BETWEEN 0 AND 100),
    start_minute integer NOT NULL DEFAULT 0 CHECK (start_minute BETWEEN 0 AND 1439),
    end_minute integer NOT NULL DEFAULT 1439 CHECK (end_minute BETWEEN 0 AND 1439),
    monthly_token_limit bigint NOT NULL DEFAULT 0 CHECK (monthly_token_limit BETWEEN 0 AND 1000000000000),
    version bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE group_chat_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE group_chat_preferences FORCE ROW LEVEL SECURITY;
CREATE POLICY group_chat_scope ON group_chat_preferences USING (
    current_user='wsl_platform' OR (
        tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid
        AND EXISTS(SELECT 1 FROM group_bindings b WHERE b.group_id=group_chat_preferences.group_id
            AND b.tenant_id=group_chat_preferences.tenant_id AND b.active)
    )
);
GRANT SELECT,INSERT,UPDATE,DELETE ON group_chat_preferences TO wsl_platform;
GRANT SELECT ON group_chat_preferences TO wsl_tenant;
ALTER TABLE personas ADD COLUMN published boolean NOT NULL DEFAULT false;

CREATE TABLE lajiaoliao_auth_state (
    account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    device_id text NOT NULL,
    state text NOT NULL DEFAULT 'ready' CHECK(state IN ('ready','submitting','unknown')),
    actor_id uuid REFERENCES users(id),
    attempted_at timestamptz,
    error text NOT NULL DEFAULT '',
    stream_initialized boolean NOT NULL DEFAULT false,
    version bigint NOT NULL DEFAULT 1
);
CREATE TABLE lajiaoliao_cursors (
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    conversation text NOT NULL CHECK(length(conversation) BETWEEN 1 AND 128),
    sequence bigint NOT NULL CHECK(sequence>=0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(account_id,conversation)
);
GRANT SELECT,INSERT,UPDATE,DELETE ON lajiaoliao_auth_state,lajiaoliao_cursors TO wsl_platform;
REVOKE ALL ON lajiaoliao_auth_state,lajiaoliao_cursors FROM wsl_tenant;
DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['lajiaoliao_auth_state','lajiaoliao_cursors']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
        EXECUTE format('CREATE POLICY platform_only ON %I USING(current_user=''wsl_platform'') WITH CHECK(current_user=''wsl_platform'')',t);
    END LOOP;
END $$;
