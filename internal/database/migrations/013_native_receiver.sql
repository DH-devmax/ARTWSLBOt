-- Receive-only runtime. These flags do not authorize moderation or AI sends.
CREATE TABLE native_im_profile (
    singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
    sealed bytea NOT NULL,
    version bigint NOT NULL DEFAULT 1
);
CREATE TABLE account_receivers (
    account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    enabled boolean NOT NULL DEFAULT false,
    revision bigint NOT NULL DEFAULT 1,
    cursor numeric(20,0) NOT NULL DEFAULT 0 CHECK(cursor BETWEEN 0 AND 18446744073709551615),
    owner text NOT NULL DEFAULT '',
    lease_until timestamptz,
    observed_at timestamptz,
    snapshot jsonb NOT NULL DEFAULT '{}'
);
-- NIM targets have not yet been mapped to verified business group IDs. Never
-- insert them into native_groups or dispatch their contents as management work.
CREATE TABLE native_received_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    event_key text NOT NULL CHECK(length(event_key) BETWEEN 1 AND 300),
    kind text NOT NULL CHECK(kind IN ('group','private','boundary','notice','quarantined')),
    received_at timestamptz NOT NULL,
    payload_sealed bytea,
    UNIQUE(account_id,event_key)
);
CREATE INDEX native_events_retention ON native_received_events(received_at);
GRANT SELECT,INSERT,UPDATE,DELETE ON native_im_profile,account_receivers,native_received_events TO wsl_platform;
GRANT USAGE,SELECT ON SEQUENCE native_received_events_id_seq TO wsl_platform;
REVOKE ALL ON native_im_profile,account_receivers,native_received_events FROM wsl_tenant;
