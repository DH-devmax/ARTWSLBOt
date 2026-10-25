BEGIN;
DO $$
BEGIN
    IF current_database() <> 'wsl' OR EXISTS (
        SELECT 1 FROM accounts a WHERE name IN ('ui-smoke-A-20261025','ui-smoke-B-20261025')
        AND (native_uid IS NOT NULL OR connection_enabled OR EXISTS (
            SELECT 1 FROM group_bindings b WHERE b.primary_account_id=a.id OR b.ai_account_id=a.id
        ))
    ) THEN
        RAISE EXCEPTION 'Refusing cleanup outside the empty local UI fixture scope';
    END IF;
END $$;
DELETE FROM personas WHERE account_id IN (
    SELECT id FROM accounts WHERE name IN ('ui-smoke-A-20261025','ui-smoke-B-20261025')
);
DELETE FROM audit_events WHERE resource_id IN (
    SELECT id::text FROM accounts WHERE name IN ('ui-smoke-A-20261025','ui-smoke-B-20261025')
) AND action IN ('account_created','account_updated','persona_updated');
DELETE FROM accounts WHERE name IN ('ui-smoke-A-20261025','ui-smoke-B-20261025');
COMMIT;
