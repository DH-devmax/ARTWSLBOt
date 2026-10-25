ALTER TABLE accounts ADD COLUMN workspace text NOT NULL DEFAULT 'wsl'
    CHECK (workspace IN ('wsl','maibot'));
GRANT SELECT(workspace) ON accounts TO wsl_tenant;
CREATE INDEX accounts_workspace ON accounts(workspace,created_at);
ALTER POLICY visible_accounts ON accounts USING (
    current_user='wsl_platform' OR (workspace='wsl' AND EXISTS (
        SELECT 1 FROM group_bindings b WHERE b.primary_account_id=accounts.id
    ))
);
REVOKE SELECT ON personas FROM wsl_tenant;

CREATE TABLE user_access (
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    role_code text NOT NULL CHECK (role_code IN ('manager','operator','viewer','custom')),
    permissions text[] NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now()
);
GRANT SELECT,INSERT,UPDATE ON user_access TO wsl_platform;

ALTER TABLE group_bindings ADD COLUMN ai_requested boolean NOT NULL DEFAULT false;
ALTER TABLE group_bindings ADD COLUMN ai_request_state text NOT NULL DEFAULT 'disabled'
    CHECK (ai_request_state IN ('disabled','pending_verification','pending_invite','ready'));

CREATE FUNCTION enforce_account_workspace() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' OR NEW.primary_account_id IS DISTINCT FROM OLD.primary_account_id THEN
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
CREATE TRIGGER binding_account_workspace BEFORE INSERT OR UPDATE ON group_bindings
    FOR EACH ROW EXECUTE FUNCTION enforce_account_workspace();
