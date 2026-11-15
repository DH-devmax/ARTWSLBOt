-- Existing grants are retained. No tenant or production action is admitted by
-- migration: ceilings, gates and group runtime require explicit administration.
CREATE TABLE tenant_capability_ceilings (
    tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
    permissions text[] NOT NULL DEFAULT '{}' CHECK(cardinality(permissions)<=256),
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    updated_by uuid REFERENCES users(id),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE business_capability_gates (
    platform text NOT NULL CHECK(platform IN ('wsl','lajiaoliao')),
    action text NOT NULL CHECK(action IN ('group_info','publish_notice','member_nickname',
        'mute_member','unmute_member','mute_group','unmute_group','kick_member',
        'send_text','recall_message','group_accept')),
    source text NOT NULL CHECK(source IN ('assistant','automatic','onboarding','heatbot')),
    state text NOT NULL DEFAULT 'closed' CHECK(state IN ('closed','pilot','open')),
    evidence_refs text[] NOT NULL DEFAULT '{}' CHECK(cardinality(evidence_refs)<=16),
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    updated_by uuid REFERENCES users(id),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(platform,action,source),
    CHECK(state='closed' OR cardinality(evidence_refs)>0)
);
CREATE TABLE business_capability_pilots (
    platform text NOT NULL,
    action text NOT NULL,
    source text NOT NULL,
    group_id uuid NOT NULL REFERENCES native_groups(id) ON DELETE CASCADE,
    gate_version bigint NOT NULL CHECK(gate_version>0),
    PRIMARY KEY(platform,action,source,group_id),
    FOREIGN KEY(platform,action,source) REFERENCES business_capability_gates(platform,action,source)
);
CREATE TABLE group_business_runtime (
    group_id uuid PRIMARY KEY REFERENCES native_groups(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    enabled boolean NOT NULL DEFAULT false,
    enabled_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    updated_by uuid REFERENCES users(id),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO group_business_runtime(group_id,tenant_id)
    SELECT group_id,tenant_id FROM group_bindings;

CREATE FUNCTION business_runtime_binding_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' OR (NEW.tenant_id,NEW.primary_account_id,NEW.active,NEW.management_enabled)
        IS DISTINCT FROM (OLD.tenant_id,OLD.primary_account_id,OLD.active,OLD.management_enabled) THEN
        INSERT INTO group_business_runtime(group_id,tenant_id,enabled)
            VALUES(NEW.group_id,NEW.tenant_id,false)
            ON CONFLICT(group_id) DO UPDATE SET tenant_id=excluded.tenant_id,enabled=false,
                version=group_business_runtime.version+1,updated_by=NULL,updated_at=clock_timestamp();
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER business_runtime_binding_changed AFTER INSERT OR UPDATE ON group_bindings
    FOR EACH ROW EXECUTE FUNCTION business_runtime_binding_changed();

CREATE FUNCTION business_runtime_consistent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM group_bindings b WHERE b.group_id=NEW.group_id
        AND b.tenant_id=NEW.tenant_id AND (NOT NEW.enabled OR b.active AND b.primary_account_id IS NOT NULL)) THEN
        RAISE EXCEPTION 'business runtime binding mismatch' USING ERRCODE='23514';
    END IF;
    IF NOT NEW.enabled THEN
        NEW.enabled_at := NULL;
    ELSIF TG_OP='INSERT' THEN
        NEW.enabled_at := clock_timestamp();
    ELSIF NOT OLD.enabled OR OLD.enabled_at IS NULL THEN
        NEW.enabled_at := clock_timestamp();
    ELSE
        NEW.enabled_at := OLD.enabled_at;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER business_runtime_consistent BEFORE INSERT OR UPDATE ON group_business_runtime
    FOR EACH ROW EXECUTE FUNCTION business_runtime_consistent();

CREATE FUNCTION business_pilot_consistent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM native_groups g WHERE g.id=NEW.group_id AND g.platform=NEW.platform) THEN
        RAISE EXCEPTION 'business pilot platform mismatch' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER business_pilot_consistent BEFORE INSERT OR UPDATE ON business_capability_pilots
    FOR EACH ROW EXECUTE FUNCTION business_pilot_consistent();

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['tenant_capability_ceilings','business_capability_gates',
        'business_capability_pilots','group_business_runtime'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
        EXECUTE format('CREATE POLICY platform_only ON %I USING(current_user=''wsl_platform'') WITH CHECK(current_user=''wsl_platform'')',t);
        EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I TO wsl_platform',t);
    END LOOP;
END $$;
