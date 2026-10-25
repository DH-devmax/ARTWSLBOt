CREATE TABLE native_signup_attempts (
    transaction_id text PRIMARY KEY CHECK (length(transaction_id) BETWEEN 32 AND 128),
    account_id uuid NOT NULL REFERENCES accounts(id),
    actor_id uuid NOT NULL REFERENCES users(id),
    deployment_version bigint NOT NULL,
    account_version bigint NOT NULL,
    state text NOT NULL CHECK (state IN ('submitted','registered','rejected')),
    registered_account text CHECK (registered_account ~ '^[1-9][0-9]{0,9}$'),
    result_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((state = 'registered') = (registered_account IS NOT NULL))
);
CREATE UNIQUE INDEX native_signup_account_guard ON native_signup_attempts(account_id)
    WHERE state IN ('submitted','registered');
CREATE UNIQUE INDEX native_signup_identity_guard ON native_signup_attempts(registered_account)
    WHERE registered_account IS NOT NULL;
GRANT SELECT,INSERT,UPDATE ON native_signup_attempts TO wsl_platform;
REVOKE ALL ON native_signup_attempts FROM wsl_tenant;
