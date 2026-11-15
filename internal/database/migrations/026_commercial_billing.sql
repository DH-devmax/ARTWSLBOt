CREATE TABLE commercial_plans (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code text NOT NULL CHECK(code IN ('PLUS','PRO')),
    version bigint NOT NULL CHECK(version>0),
    plan_id uuid NOT NULL UNIQUE REFERENCES plans(id),
    price_micro_usdt bigint NOT NULL CHECK(price_micro_usdt>0),
    credits_micro bigint NOT NULL CHECK(credits_micro>=0),
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(code,version)
);
WITH p AS (
 INSERT INTO plans(name,group_limit,ai_group_limit,monthly_ai_tokens,monthly_assistant_tokens,
    modules,allowed_platforms,member_limit)
 VALUES('PLUS 29 USDT / 30 days',1,0,0,0,
    ARRAY['moderation','replies','schedules','rankings','cards','friends','activities','finance'],
    ARRAY['wsl','lajiaoliao'],3) RETURNING id
) INSERT INTO commercial_plans(code,version,plan_id,price_micro_usdt,credits_micro)
 SELECT 'PLUS',1,id,29000000,0 FROM p;
WITH p AS (
 INSERT INTO plans(name,group_limit,ai_group_limit,monthly_ai_tokens,monthly_assistant_tokens,
    modules,allowed_platforms,member_limit)
 VALUES('PRO 49 USDT / 30 days',3,3,0,0,
    ARRAY['moderation','replies','schedules','rankings','cards','friends','activities','finance','ai','management_assistant'],
    ARRAY['wsl','lajiaoliao'],10) RETURNING id
) INSERT INTO commercial_plans(code,version,plan_id,price_micro_usdt,credits_micro)
 SELECT 'PRO',1,id,49000000,10000000000 FROM p;

CREATE TABLE subscription_orders (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    plan_version_id uuid NOT NULL REFERENCES commercial_plans(id),
    idempotency_key text NOT NULL UNIQUE CHECK(length(idempotency_key) BETWEEN 8 AND 100),
    payment_reference text NOT NULL UNIQUE CHECK(length(payment_reference) BETWEEN 3 AND 120),
    amount_micro_usdt bigint NOT NULL CHECK(amount_micro_usdt>0),
    confirmed_by uuid NOT NULL REFERENCES users(id),
    confirmed_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE subscription_periods (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    order_id uuid NOT NULL UNIQUE REFERENCES subscription_orders(id),
    plan_version_id uuid NOT NULL REFERENCES commercial_plans(id),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    state text NOT NULL DEFAULT 'scheduled' CHECK(state IN ('scheduled','active','expired','blocked')),
    reason text NOT NULL DEFAULT '',
    CHECK(ends_at=starts_at+interval '720 hours'),
    UNIQUE(tenant_id,starts_at)
);
CREATE TABLE ai_wallets (
    period_id uuid PRIMARY KEY REFERENCES subscription_periods(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    granted_micro bigint NOT NULL CHECK(granted_micro>=0),
    spent_micro bigint NOT NULL DEFAULT 0 CHECK(spent_micro>=0),
    held_micro bigint NOT NULL DEFAULT 0 CHECK(held_micro>=0),
    CHECK(spent_micro+held_micro<=granted_micro)
);
CREATE TABLE credit_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid REFERENCES tenants(id),
    period_id uuid REFERENCES subscription_periods(id),
    call_id uuid,
    actor_id uuid REFERENCES users(id),
    kind text NOT NULL CHECK(kind IN ('grant','charge','release','adjustment')),
    amount_micro bigint NOT NULL,
    reason text NOT NULL DEFAULT '',
    external_key text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE ai_credit_limits (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    kind text NOT NULL CHECK(kind IN ('purpose','group','member')),
    scope text NOT NULL,
    limit_micro bigint NOT NULL CHECK(limit_micro>=0),
    version bigint NOT NULL DEFAULT 1,
    PRIMARY KEY(tenant_id,kind,scope)
);
DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['commercial_plans','subscription_orders','subscription_periods','ai_wallets','credit_entries','ai_credit_limits'] LOOP
        EXECUTE format('GRANT SELECT,INSERT,UPDATE ON %I TO wsl_platform',t);
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
        EXECUTE format('CREATE POLICY platform_only ON %I USING(current_user=''wsl_platform'')',t);
    END LOOP;
END $$;
REVOKE UPDATE,DELETE ON subscription_orders,credit_entries FROM wsl_platform;
CREATE FUNCTION immutable_billing_record() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'immutable billing record' USING ERRCODE='23514'; END $$;
CREATE TRIGGER immutable_order BEFORE UPDATE OR DELETE ON subscription_orders FOR EACH ROW EXECUTE FUNCTION immutable_billing_record();
CREATE TRIGGER immutable_credit BEFORE UPDATE OR DELETE ON credit_entries FOR EACH ROW EXECUTE FUNCTION immutable_billing_record();
CREATE FUNCTION preserve_commercial_plan() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' OR (to_jsonb(NEW)-'enabled') IS DISTINCT FROM (to_jsonb(OLD)-'enabled') THEN
        RAISE EXCEPTION 'publish a new commercial plan version' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER immutable_commercial_plan BEFORE UPDATE OR DELETE ON commercial_plans
    FOR EACH ROW EXECUTE FUNCTION preserve_commercial_plan();
CREATE FUNCTION preserve_commercial_entitlements() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS(SELECT 1 FROM commercial_plans WHERE plan_id=OLD.id) THEN
        RAISE EXCEPTION 'publish a new commercial plan version' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER immutable_commercial_entitlements BEFORE UPDATE OR DELETE ON plans
    FOR EACH ROW EXECUTE FUNCTION preserve_commercial_entitlements();
