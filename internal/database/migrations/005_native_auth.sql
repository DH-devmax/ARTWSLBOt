CREATE TABLE native_deployment (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    sealed bytea NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now()
);
GRANT SELECT,INSERT,UPDATE ON native_deployment TO wsl_platform;
REVOKE ALL ON native_deployment FROM wsl_tenant;

ALTER TABLE accounts ADD COLUMN auth_state text NOT NULL DEFAULT 'not_authenticated';
ALTER TABLE accounts ADD COLUMN authenticated_at timestamptz;
ALTER TABLE accounts ADD COLUMN native_nickname text NOT NULL DEFAULT '';
GRANT SELECT(auth_state,authenticated_at,native_nickname) ON accounts TO wsl_tenant;
