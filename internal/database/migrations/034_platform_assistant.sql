ALTER TABLE ai_calls DROP CONSTRAINT ai_calls_purpose_check;
ALTER TABLE ai_calls ADD CONSTRAINT ai_calls_purpose_check
    CHECK(purpose IN ('assistant','heatbot','test','platform_assistant'));

CREATE TABLE platform_assistant_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
    enabled boolean NOT NULL DEFAULT false,
    monthly_limit_micro bigint NOT NULL DEFAULT 0 CHECK(monthly_limit_micro BETWEEN 0 AND 100000000000),
    version bigint NOT NULL DEFAULT 0
);
INSERT INTO platform_assistant_settings(singleton) VALUES(true);

CREATE TABLE platform_assistant_sessions (
    id uuid PRIMARY KEY,
    actor_id uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '24 hours'
);
CREATE INDEX platform_assistant_session_actor ON platform_assistant_sessions(actor_id,expires_at);
CREATE TABLE platform_assistant_turns (
    id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES platform_assistant_sessions(id) ON DELETE CASCADE,
    actor_id uuid NOT NULL REFERENCES users(id),
    idempotency_key uuid NOT NULL,
    input_hash bytea NOT NULL,
    body_sealed bytea NOT NULL,
    call_id uuid REFERENCES ai_calls(id),
    status text NOT NULL DEFAULT 'running' CHECK(status IN ('running','completed','failed','unknown','canceled')),
    cancel_requested boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    finished_at timestamptz,
    UNIQUE(actor_id,idempotency_key)
);
CREATE INDEX platform_assistant_turn_actor ON platform_assistant_turns(actor_id,status);
DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['platform_assistant_settings','platform_assistant_sessions','platform_assistant_turns'] LOOP
        EXECUTE format('GRANT SELECT,INSERT,UPDATE ON %I TO wsl_platform',t);
        EXECUTE format('REVOKE ALL ON %I FROM wsl_tenant',t);
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
        EXECUTE format('CREATE POLICY platform_only ON %I USING(current_user=''wsl_platform'')',t);
    END LOOP;
END $$;
GRANT DELETE ON platform_assistant_sessions,platform_assistant_turns TO wsl_platform;
