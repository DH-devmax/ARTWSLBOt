-- Temporary upload receipts, never local file bodies. Sending reuses the
-- existing native intent/nonce/receipt state machine and final authorization.
ALTER TABLE operations DROP CONSTRAINT operations_kind_check;
ALTER TABLE operations ADD CONSTRAINT operations_kind_check
    CHECK(kind IN('send_text','send_image','send_voice','send_video','send_file','recall','mute','unmute'));
CREATE TABLE native_media_uploads (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    actor_id uuid NOT NULL REFERENCES users(id),
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    group_id uuid NOT NULL REFERENCES native_groups(id),
    test_window_id uuid NOT NULL REFERENCES test_windows(id) ON DELETE CASCADE,
    idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 8 AND 100),
    request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
    kind text NOT NULL CHECK(kind IN('image','voice','video','file')),
    account_epoch bigint NOT NULL,
    binding_epoch bigint NOT NULL,
    receiver_owner text NOT NULL,
    receiver_revision bigint NOT NULL,
    status text NOT NULL CHECK(status IN('preparing','submitting','succeeded','failed','unknown','expired')),
    result_code text NOT NULL DEFAULT '',
    payload_sealed bytea,
    operation_id uuid UNIQUE REFERENCES operations(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '10 minutes',
    finished_at timestamptz,
    UNIQUE(actor_id,idempotency_key)
);
CREATE UNIQUE INDEX native_media_upload_one_active ON native_media_uploads(account_id)
    WHERE status IN('preparing','submitting');
CREATE INDEX native_media_upload_expiry ON native_media_uploads(expires_at);
ALTER TABLE native_media_uploads ENABLE ROW LEVEL SECURITY;
ALTER TABLE native_media_uploads FORCE ROW LEVEL SECURITY;
CREATE POLICY native_media_platform ON native_media_uploads TO wsl_platform USING(true) WITH CHECK(true);
REVOKE ALL ON native_media_uploads FROM PUBLIC,wsl_tenant;
GRANT SELECT,INSERT,UPDATE,DELETE ON native_media_uploads TO wsl_platform;
