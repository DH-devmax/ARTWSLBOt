CREATE TABLE controlled_onboarding_usage (
    scope_id uuid NOT NULL REFERENCES onboarding_test_scopes(id),
    scope_version bigint NOT NULL CHECK(scope_version>0),
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    group_id uuid NOT NULL REFERENCES native_groups(id),
    kind text NOT NULL CHECK(kind IN ('native','model')),
    source text NOT NULL CHECK(length(source) BETWEEN 1 AND 100),
    operation_key text NOT NULL CHECK(length(operation_key) BETWEEN 1 AND 200),
    reserved_micro_usdt bigint NOT NULL CHECK(
        kind='native' AND reserved_micro_usdt=0 OR
        kind='model' AND reserved_micro_usdt BETWEEN 1 AND 20000),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(scope_id,kind,source,operation_key)
);

CREATE FUNCTION controlled_onboarding_usage_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'immutable controlled onboarding reservation' USING ERRCODE='23514';
END $$;
CREATE TRIGGER controlled_onboarding_usage_immutable BEFORE UPDATE OR DELETE ON controlled_onboarding_usage
    FOR EACH ROW EXECUTE FUNCTION controlled_onboarding_usage_immutable();

ALTER TABLE controlled_onboarding_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE controlled_onboarding_usage FORCE ROW LEVEL SECURITY;
CREATE POLICY platform_only ON controlled_onboarding_usage
    USING(current_user='wsl_platform') WITH CHECK(current_user='wsl_platform');
GRANT SELECT,INSERT ON controlled_onboarding_usage TO wsl_platform;
