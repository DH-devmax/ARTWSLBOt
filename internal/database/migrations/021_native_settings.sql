-- Explicit manual desktop profile/group-card operations. No autonomous worker.
CREATE TABLE native_setting_operations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id uuid NOT NULL REFERENCES users(id),
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    group_id uuid REFERENCES native_groups(id),
    kind text NOT NULL CHECK(kind IN
        ('nickname','member_nickname','search','friend_policy','group_policy','audio_video')),
    idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 8 AND 100),
    request_hash bytea NOT NULL,
    payload_sealed bytea,
    account_epoch bigint NOT NULL,
    status text NOT NULL CHECK(status IN ('preparing','submitting','succeeded','failed','unknown')),
    result_code text NOT NULL DEFAULT '',
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(actor_id,idempotency_key)
);
CREATE INDEX native_setting_pending ON native_setting_operations(account_id,status)
    WHERE status IN ('preparing','submitting','unknown');
ALTER TABLE native_setting_operations ENABLE ROW LEVEL SECURITY;
ALTER TABLE native_setting_operations FORCE ROW LEVEL SECURITY;
CREATE POLICY platform_only ON native_setting_operations
    USING(current_user='wsl_platform') WITH CHECK(current_user='wsl_platform');
GRANT SELECT,INSERT,UPDATE,DELETE ON native_setting_operations TO wsl_platform;
REVOKE ALL ON native_setting_operations FROM wsl_tenant;
