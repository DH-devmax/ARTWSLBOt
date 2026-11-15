-- Changing authentication adapters cannot convert a saved native session.
-- Fence existing work and require explicit desktop authentication. Keep shared
-- identities, tenant bindings, unknown submissions and business evidence intact.
UPDATE accounts
SET auth_state='reauth_required', connection_enabled=false,
    epoch=epoch+1, version=version+1
WHERE credential_sealed IS NOT NULL OR connection_enabled;

UPDATE account_receivers
SET enabled=false, revision=revision+1, snapshot='{}'::jsonb,
    observed_at=NULL
WHERE enabled;
