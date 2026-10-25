-- Forward-only change: the former 30 AI-group cap is now 100, independently
-- admitted from the management workspace. This does not verify engine capacity.
ALTER TABLE plans DROP CONSTRAINT plans_check;
ALTER TABLE plans ADD CONSTRAINT plans_ai_group_limit_check
    CHECK (ai_group_limit BETWEEN 0 AND 100 AND ai_group_limit <= group_limit);

CREATE TABLE maibot_settings (
    account_id uuid NOT NULL REFERENCES accounts(id),
    section text NOT NULL CHECK (section IN ('chat','models','learning','memory')),
    sealed bytea NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_by uuid NOT NULL REFERENCES users(id),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, section)
);
GRANT SELECT, INSERT, UPDATE ON maibot_settings TO wsl_platform;
-- Shared robot profiles and provider credentials are platform-only.
REVOKE ALL ON maibot_settings FROM wsl_tenant;
