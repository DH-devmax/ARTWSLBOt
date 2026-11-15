-- A numeric provider diagnosis is not identity, authorization or a retry token.
-- Old attempts stay empty: their discarded provider responses cannot be restored.
ALTER TABLE group_binding_outbound_attempts
    ADD COLUMN provider_code integer CHECK(provider_code BETWEEN 1 AND 9999),
    ADD COLUMN provider_category text CHECK(provider_category IN
        ('upstream_rejected','rate_limited','session_expired','transport_unavailable','response_invalid')),
    ADD COLUMN provider_observed_at timestamptz,
    ADD CONSTRAINT group_binding_outbound_diagnostic_pair CHECK(
        (provider_code IS NULL AND provider_category IS NULL AND provider_observed_at IS NULL)
        OR (provider_code IS NOT NULL AND provider_category IS NOT NULL
            AND provider_observed_at IS NOT NULL AND attempts_used=1)
    );

CREATE FUNCTION group_binding_outbound_diagnostic_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.provider_code IS NOT NULL OR NEW.provider_category IS NOT NULL OR NEW.provider_observed_at IS NOT NULL THEN
            RAISE EXCEPTION 'outbound diagnosis requires a submission' USING ERRCODE='23514';
        END IF;
    ELSIF (NEW.provider_code,NEW.provider_category,NEW.provider_observed_at)
        IS DISTINCT FROM (OLD.provider_code,OLD.provider_category,OLD.provider_observed_at) THEN
        IF OLD.provider_code IS NOT NULL OR OLD.provider_category IS NOT NULL OR OLD.provider_observed_at IS NOT NULL
            OR OLD.status<>'submitting' OR OLD.attempts_used<>1
            OR NEW.status NOT IN ('failed','unknown')
            OR NEW.provider_code IS NULL OR NEW.provider_category IS NULL OR NEW.provider_observed_at IS NULL THEN
            RAISE EXCEPTION 'immutable outbound diagnosis' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER group_binding_outbound_diagnostic_scope BEFORE INSERT OR UPDATE
    ON group_binding_outbound_attempts FOR EACH ROW
    EXECUTE FUNCTION group_binding_outbound_diagnostic_scope();
