-- Manual desktop SMS registration/login, shared with the account login barrier.
ALTER TABLE lajiaoliao_auth_state
    ADD COLUMN sms_phone_sealed bytea,
    ADD COLUMN sms_actor_id uuid REFERENCES users(id),
    ADD COLUMN sms_session_hash bytea,
    ADD COLUMN sms_expires_at timestamptz,
    ADD COLUMN sms_resend_at timestamptz,
    ADD COLUMN sms_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN sms_phone_fingerprint text;
