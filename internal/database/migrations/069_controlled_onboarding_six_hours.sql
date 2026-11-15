-- Existing scopes retain their immutable deadlines; only new scopes may last six hours.
ALTER TABLE onboarding_test_scopes
    DROP CONSTRAINT onboarding_test_scopes_check2,
    ADD CONSTRAINT onboarding_test_scope_deadline
        CHECK(expires_at>created_at AND expires_at<=created_at+interval '6 hours 1 second'),
    ADD COLUMN request_default_minutes integer NOT NULL DEFAULT 30
        CHECK(request_default_minutes IN (30,360));

-- Old binaries retain their canonical default during a compatible rollback.
-- New writers explicitly record 360; retries use the original request default.
CREATE FUNCTION onboarding_test_scope_default_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.request_default_minutes IS DISTINCT FROM OLD.request_default_minutes THEN
        RAISE EXCEPTION 'immutable controlled onboarding request default' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER onboarding_test_scope_default_immutable
    BEFORE UPDATE OF request_default_minutes ON onboarding_test_scopes
    FOR EACH ROW EXECUTE FUNCTION onboarding_test_scope_default_immutable();
