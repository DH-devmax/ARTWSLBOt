-- Forward delivery of the previously undeployed Chili media candidate.
-- Workspaces which applied 041 already have the identical platform-only table.
DO $$
BEGIN
    IF EXISTS(SELECT 1 FROM schema_migrations WHERE name='041_lajiaoliao_media_attempts.sql') THEN
        IF to_regclass('public.lajiaoliao_media_attempts') IS NULL THEN
            RAISE EXCEPTION 'Chili attempt table missing after recorded migration';
        END IF;
        RETURN;
    END IF;
    CREATE TABLE lajiaoliao_media_attempts (
        operation_id uuid PRIMARY KEY REFERENCES operations(id) ON DELETE CASCADE,
        client_id text NOT NULL UNIQUE CHECK(length(client_id) BETWEEN 1 AND 128),
        native_sender text NOT NULL CHECK(length(native_sender) BETWEEN 1 AND 128),
        native_group text NOT NULL CHECK(length(native_group) BETWEEN 1 AND 128),
        content_digest bytea NOT NULL CHECK(octet_length(content_digest)=32),
        submitted_at timestamptz,
        receipt_sealed bytea
    );
    ALTER TABLE lajiaoliao_media_attempts ENABLE ROW LEVEL SECURITY;
    ALTER TABLE lajiaoliao_media_attempts FORCE ROW LEVEL SECURITY;
    CREATE POLICY lajiaoliao_media_platform ON lajiaoliao_media_attempts
        TO wsl_platform USING(true) WITH CHECK(true);
    REVOKE ALL ON lajiaoliao_media_attempts FROM PUBLIC,wsl_tenant;
    GRANT SELECT,INSERT,UPDATE,DELETE ON lajiaoliao_media_attempts TO wsl_platform;
END;
$$;
