-- Dormant native text foundation. No production worker or API activation.
-- Do not reset this high-water mark on account/deployment deletion or restore.
CREATE TABLE native_message_nonce (
    singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
    value numeric(20,0) NOT NULL CHECK(value BETWEEN 0 AND 18446744073709551615)
);
INSERT INTO native_message_nonce VALUES(true,0);
CREATE TABLE native_text_attempts (
    operation_id uuid PRIMARY KEY REFERENCES operations(id) ON DELETE CASCADE,
    client_id uuid NOT NULL UNIQUE,
    nonce numeric(20,0) NOT NULL UNIQUE CHECK(nonce BETWEEN 1 AND 18446744073709551615),
    aad numeric(20,0) NOT NULL CHECK(aad BETWEEN 1 AND 18446744073709551615),
    message_at timestamptz NOT NULL,
    server_id numeric(20,0) CHECK(server_id BETWEEN 1 AND 18446744073709551615),
    server_time_ms numeric(20,0) CHECK(server_time_ms BETWEEN 1 AND 18446744073709551615),
    submitted_at timestamptz,
    finished_at timestamptz,
    CHECK((server_id IS NULL) = (server_time_ms IS NULL))
);
GRANT SELECT,INSERT,UPDATE ON native_message_nonce,native_text_attempts TO wsl_platform;
REVOKE ALL ON native_message_nonce,native_text_attempts FROM wsl_tenant;
