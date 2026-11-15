-- A companion business session belongs to an existing WSL pool identity.
-- It never supplies IM credentials or owns a receive connection.
CREATE TABLE native_mobile_deployment (
    singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
    sealed bytea NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE native_mobile_sessions (
    account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    native_uid text NOT NULL,
    account_epoch bigint NOT NULL CHECK(account_epoch>0),
    deployment_version bigint NOT NULL CHECK(deployment_version>0),
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    request_count bigint NOT NULL DEFAULT 0 CHECK(request_count>=0 AND request_count<4294967296),
    credential_sealed bytea,
    state text NOT NULL CHECK(state IN ('authenticated','reauth_required','revoked')),
    authenticated_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK((state='authenticated')=(credential_sealed IS NOT NULL))
);

GRANT SELECT,INSERT,UPDATE ON native_mobile_deployment,native_mobile_sessions TO wsl_platform;
REVOKE ALL ON native_mobile_deployment,native_mobile_sessions FROM wsl_tenant;
