-- Server-owned routing evidence; no body, credentials or tenant-readable data.
CREATE TABLE native_text_routes (
    operation_id uuid PRIMARY KEY REFERENCES native_text_attempts(operation_id) ON DELETE CASCADE,
    team text NOT NULL CHECK (length(team) BETWEEN 1 AND 20),
    sender text NOT NULL CHECK (length(sender) BETWEEN 1 AND 128),
    deployment_version bigint NOT NULL CHECK (deployment_version > 0),
    attachment_digest bytea NOT NULL CHECK (octet_length(attachment_digest)=32),
    observed_at timestamptz NOT NULL,
    reconciled_event_id bigint
);
REVOKE ALL ON native_text_routes FROM PUBLIC, wsl_tenant;
GRANT SELECT,INSERT,UPDATE ON native_text_routes TO wsl_platform;
