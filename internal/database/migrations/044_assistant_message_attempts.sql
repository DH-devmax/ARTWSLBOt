-- Independent business attempts; no test-window grants or Chili media tables.
DO $$
DECLARE previous text;
BEGIN
    SELECT pg_get_expr(conbin,conrelid) INTO STRICT previous FROM pg_constraint
      WHERE conrelid='native_setting_operations'::regclass AND conname='native_setting_operations_kind_check';
    ALTER TABLE native_setting_operations DROP CONSTRAINT native_setting_operations_kind_check;
    EXECUTE format('ALTER TABLE native_setting_operations ADD CONSTRAINT native_setting_operations_kind_check
      CHECK ((%s) OR kind IN (''group_info'',''send_text'',''recall_message''))', previous);
END $$;
CREATE TABLE assistant_message_attempts (
    operation_id uuid PRIMARY KEY REFERENCES native_setting_operations(id) ON DELETE CASCADE,
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    client_id uuid NOT NULL UNIQUE,
    receiver_owner text NOT NULL CHECK(receiver_owner<>''),
    receiver_revision bigint NOT NULL CHECK(receiver_revision>0),
    nonce numeric(20,0) UNIQUE,
    aad numeric(20,0),
    message_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    route_digest bytea,
    route_deployment bigint,
    CHECK((platform='wsl' AND nonce IS NOT NULL AND aad IS NOT NULL AND nonce BETWEEN 1 AND 18446744073709551615
           AND aad BETWEEN 1 AND 18446744073709551615)
       OR (platform='lajiaoliao' AND nonce IS NULL AND aad IS NULL)),
    CHECK((route_digest IS NULL AND route_deployment IS NULL) OR
          (route_digest IS NOT NULL AND octet_length(route_digest)=32 AND route_deployment IS NOT NULL AND route_deployment>0))
);
GRANT SELECT,INSERT,UPDATE ON assistant_message_attempts TO wsl_platform;
REVOKE ALL ON assistant_message_attempts FROM wsl_tenant;

CREATE FUNCTION assistant_attempt_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.operation_id,NEW.platform,NEW.client_id,NEW.receiver_owner,NEW.receiver_revision,
        NEW.nonce,NEW.aad,NEW.message_at)
        IS DISTINCT FROM
       (OLD.operation_id,OLD.platform,OLD.client_id,OLD.receiver_owner,OLD.receiver_revision,
        OLD.nonce,OLD.aad,OLD.message_at) OR
       (OLD.route_digest IS NOT NULL AND
        (NEW.route_digest,NEW.route_deployment) IS DISTINCT FROM (OLD.route_digest,OLD.route_deployment)) THEN
        RAISE EXCEPTION 'assistant message identity is immutable';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER assistant_attempt_immutable BEFORE UPDATE ON assistant_message_attempts
FOR EACH ROW EXECUTE FUNCTION assistant_attempt_immutable();
