-- Read-only, platform-owned projections. Native observations never authorize a
-- SaaS group. All plaintext bodies remain sealed; expiry follows the raw event.
CREATE TABLE native_decoded_events (
    event_id bigint PRIMARY KEY REFERENCES native_received_events(id) ON DELETE CASCADE,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    state text NOT NULL CHECK(state IN ('processing','decoded','unsupported','failed')),
    scene text NOT NULL DEFAULT '' CHECK(scene IN ('','group','private')),
    conversation_key text NOT NULL DEFAULT '',
    reason text NOT NULL DEFAULT '',
    owner text NOT NULL DEFAULT '',
    lease_until timestamptz,
    deployment_version bigint NOT NULL,
    codec_version integer NOT NULL DEFAULT 1,
    payload_sealed bytea,
    received_at timestamptz NOT NULL,
    decoded_at timestamptz,
    decode_ms double precision CHECK(decode_ms>=0),
    attempts integer NOT NULL DEFAULT 1 CHECK(attempts>0)
);
CREATE INDEX native_decoded_conversation ON native_decoded_events(account_id,scene,conversation_key,event_id DESC)
    WHERE state IN ('decoded','unsupported') AND conversation_key<>'';
CREATE INDEX native_decode_account_state ON native_decoded_events(account_id,state);
CREATE INDEX native_events_decode_source ON native_received_events(account_id,id)
    WHERE kind IN ('group','private');

CREATE TABLE native_directory_snapshots (
    account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    payload_sealed bytea,
    sampled_at timestamptz,
    requested_at timestamptz NOT NULL DEFAULT now(),
    attempted_at timestamptz,
    retry_at timestamptz,
    error text NOT NULL DEFAULT '',
    group_count integer NOT NULL DEFAULT 0 CHECK(group_count>=0)
);
GRANT SELECT,INSERT,UPDATE,DELETE ON native_decoded_events,native_directory_snapshots TO wsl_platform;
REVOKE ALL ON native_decoded_events,native_directory_snapshots FROM wsl_tenant;
