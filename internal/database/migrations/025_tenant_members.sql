-- Existing users retain explicit legacy grants, but are never guessed to be owners.
ALTER TABLE users ADD COLUMN tenant_role text NOT NULL DEFAULT 'legacy'
    CHECK (tenant_role ~ '^[a-z0-9_]{1,40}$' AND tenant_role<>'superadmin');
ALTER TABLE users ADD COLUMN access_version bigint NOT NULL DEFAULT 1;
ALTER TABLE tenants ADD COLUMN owner_id uuid REFERENCES users(id);
GRANT DELETE ON user_access TO wsl_platform;
ALTER TABLE plans ADD COLUMN member_limit integer NOT NULL DEFAULT 3 CHECK(member_limit BETWEEN 1 AND 100);
INSERT INTO user_access(user_id,role_code,permissions)
SELECT id,'manager',ARRAY[
 'overview:read','accounts:read','groups:read','groups:configure','groups:moderation','groups:replies',
 'groups:revoke','groups:ai','claims:read','claims:create','operations:read','operations:create',
 'operations:cancel','ledger:read','ledger:create','activities:read','activities:create','activities:draw','audit:read',
 'feature:blocked_words:enabled','feature:blocked_words:words','feature:blacklist:enabled','feature:blacklist:members',
 'feature:exemptions:enabled','feature:exemptions:members','feature:keyword_replies:enabled',
 'feature:keyword_replies:admin_only','feature:keyword_replies:rules'
] FROM users WHERE role='user' ON CONFLICT(user_id) DO NOTHING;

CREATE TABLE tenant_roles (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    code text NOT NULL CHECK(length(code) BETWEEN 1 AND 40),
    name text NOT NULL CHECK(length(name) BETWEEN 1 AND 80),
    permissions text[] NOT NULL DEFAULT '{}',
    version bigint NOT NULL DEFAULT 1,
    PRIMARY KEY(tenant_id,code)
);
CREATE TABLE member_overrides (
    user_id uuid NOT NULL REFERENCES users(id),
    permission text NOT NULL,
    effect text NOT NULL CHECK(effect IN ('allow','deny')),
    expires_at timestamptz,
    PRIMARY KEY(user_id,permission)
);
CREATE TABLE member_groups (
    user_id uuid NOT NULL REFERENCES users(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    group_id uuid NOT NULL REFERENCES native_groups(id),
    PRIMARY KEY(user_id,group_id)
);
INSERT INTO member_groups(user_id,tenant_id,group_id)
    SELECT u.id,u.tenant_id,b.group_id FROM users u JOIN group_bindings b ON b.tenant_id=u.tenant_id
    WHERE u.role='user';

-- A sealed grant is not needed here: this is authorization metadata, never credentials.
CREATE FUNCTION member_group_visible(t uuid,g uuid,a uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,public AS $$
    SELECT EXISTS(SELECT 1 FROM public.users u WHERE u.id=a AND u.tenant_id=t
        AND u.role='user' AND NOT u.disabled AND (
          u.tenant_role='owner' AND EXISTS(SELECT 1 FROM public.tenants x WHERE x.id=t AND x.owner_id=a)
          OR EXISTS(SELECT 1 FROM public.member_groups m WHERE m.user_id=a AND m.tenant_id=t AND m.group_id=g)
        ))
$$;
REVOKE ALL ON FUNCTION member_group_visible(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION member_group_visible(uuid,uuid,uuid) TO wsl_platform,wsl_tenant;

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['tenant_roles','member_overrides','member_groups'] LOOP
        EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I TO wsl_platform',t);
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
        EXECUTE format('CREATE POLICY platform_only ON %I USING(current_user=''wsl_platform'')',t);
    END LOOP;
    FOREACH t IN ARRAY ARRAY['group_bindings','operations','ledger_entries','activities','group_chat_preferences'] LOOP
        EXECUTE format('CREATE POLICY member_scope ON %I AS RESTRICTIVE USING
            (current_user=''wsl_platform'' OR member_group_visible(tenant_id,group_id,
             nullif(current_setting(''app.actor_id'',true),'''')::uuid))',t);
    END LOOP;
END $$;
CREATE POLICY member_claim_scope ON group_claims AS RESTRICTIVE USING (
    current_user='wsl_platform' OR applicant_id=nullif(current_setting('app.actor_id',true),'')::uuid
    OR member_group_visible(tenant_id,group_id,nullif(current_setting('app.actor_id',true),'')::uuid)
);
CREATE POLICY member_audit_scope ON audit_events AS RESTRICTIVE USING (
    current_user='wsl_platform' OR actor_id=nullif(current_setting('app.actor_id',true),'')::uuid
    OR member_group_visible(tenant_id,
        CASE WHEN resource_id ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$' THEN resource_id::uuid ELSE NULL END,
        nullif(current_setting('app.actor_id',true),'')::uuid)
);
CREATE POLICY member_activity_scope ON activity_members AS RESTRICTIVE USING (
    current_user='wsl_platform' OR EXISTS(SELECT 1 FROM activities a WHERE a.id=activity_members.activity_id)
);

CREATE FUNCTION enforce_member_assignment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM users u JOIN group_bindings b ON b.tenant_id=u.tenant_id
        WHERE u.id=NEW.user_id AND b.group_id=NEW.group_id AND b.tenant_id=NEW.tenant_id) THEN
        RAISE EXCEPTION 'member group tenant mismatch' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER member_assignment BEFORE INSERT OR UPDATE ON member_groups
    FOR EACH ROW EXECUTE FUNCTION enforce_member_assignment();

CREATE FUNCTION revoke_changed_member() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.disabled IS DISTINCT FROM OLD.disabled OR NEW.password_hash IS DISTINCT FROM OLD.password_hash
        OR NEW.tenant_role IS DISTINCT FROM OLD.tenant_role OR NEW.access_version<>OLD.access_version THEN
        DELETE FROM sessions WHERE user_id=NEW.id;
        UPDATE operations SET status='cancelled',result_code='member_access_changed',updated_at=now()
            WHERE actor_id=NEW.id AND status='queued';
        UPDATE assistant_turns SET cancel_requested=true WHERE actor_id=NEW.id AND status IN ('queued','running');
    END IF;
    IF OLD.tenant_role='owner' AND (NEW.disabled OR NEW.tenant_role<>'owner') AND
        EXISTS(SELECT 1 FROM tenants WHERE id=OLD.tenant_id AND owner_id=OLD.id) THEN
        RAISE EXCEPTION 'last tenant owner protected' USING ERRCODE='23514',CONSTRAINT='last_tenant_owner';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER member_changed AFTER UPDATE ON users FOR EACH ROW EXECUTE FUNCTION revoke_changed_member();
