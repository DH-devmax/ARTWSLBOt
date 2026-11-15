ALTER TABLE native_setting_operations DROP CONSTRAINT native_setting_operations_kind_check;
ALTER TABLE native_setting_operations ADD CONSTRAINT native_setting_operations_kind_check CHECK(kind IN
    ('nickname','member_nickname','search','friend_policy','group_policy','audio_video','avatar',
     'password_sms','password_verify','password','phone_old_sms','phone_old_verify','phone_new_sms','phone'));

-- Only opaque verification keys/certificates and the new phone are sealed here.
-- Passwords, entered OTPs and images never enter this table.
CREATE TABLE native_security_flows (
    id uuid PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    actor_id uuid NOT NULL REFERENCES users(id),
    workspace text NOT NULL,
    account_epoch bigint NOT NULL,
    stage text NOT NULL,
    payload_sealed bytea,
    expires_at timestamptz NOT NULL DEFAULT now()+interval '10 minutes',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX native_security_expiry ON native_security_flows(expires_at);
ALTER TABLE native_security_flows ENABLE ROW LEVEL SECURITY;
ALTER TABLE native_security_flows FORCE ROW LEVEL SECURITY;
CREATE POLICY platform_only ON native_security_flows
    USING(current_user='wsl_platform') WITH CHECK(current_user='wsl_platform');
GRANT SELECT,INSERT,UPDATE,DELETE ON native_security_flows TO wsl_platform;
REVOKE ALL ON native_security_flows FROM wsl_tenant;
