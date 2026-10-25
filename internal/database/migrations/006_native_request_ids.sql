-- Reserve the original protocol's nonce before sending. Do not reset on config replacement.
ALTER TABLE native_deployment ADD COLUMN request_id_ms bigint NOT NULL DEFAULT 0 CHECK (request_id_ms >= 0);
ALTER TABLE native_deployment ADD COLUMN request_id_ns bigint NOT NULL DEFAULT 0 CHECK (request_id_ns >= 0);
