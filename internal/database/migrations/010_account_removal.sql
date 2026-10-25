-- Retain tenant ownership and immutable financial evidence after its robot is
-- removed. A detached binding cannot run management, chat or invitation work.
ALTER TABLE group_bindings ALTER COLUMN primary_account_id DROP NOT NULL;
ALTER TABLE group_bindings ADD CONSTRAINT detached_binding_disabled CHECK (
    primary_account_id IS NOT NULL OR
    (NOT active AND NOT management_enabled AND NOT ai_enabled AND NOT ai_requested
        AND ai_account_id IS NULL)
);
CREATE OR REPLACE FUNCTION enforce_account_workspace() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.primary_account_id IS NOT NULL AND
        (TG_OP='INSERT' OR NEW.primary_account_id IS DISTINCT FROM OLD.primary_account_id) THEN
        IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.primary_account_id AND workspace='wsl') THEN
            RAISE EXCEPTION 'management account workspace mismatch' USING ERRCODE='23514';
        END IF;
    END IF;
    IF NEW.ai_account_id IS NOT NULL AND
        (TG_OP='INSERT' OR NEW.ai_account_id IS DISTINCT FROM OLD.ai_account_id) THEN
        IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.ai_account_id AND workspace='maibot') THEN
            RAISE EXCEPTION 'chat account workspace mismatch' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
GRANT DELETE ON native_signup_attempts, maibot_settings TO wsl_platform;
CREATE INDEX inbox_account_cleanup ON inbox(account_id);
CREATE INDEX operations_account_cleanup ON operations(account_id);
CREATE INDEX test_windows_account_cleanup ON test_windows USING gin(account_ids);
