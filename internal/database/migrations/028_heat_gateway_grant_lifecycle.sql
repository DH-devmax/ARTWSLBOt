-- Short-lived authorization metadata must not prevent account removal.
ALTER TABLE heat_gateway_grants DROP CONSTRAINT heat_gateway_grants_account_id_fkey;
ALTER TABLE heat_gateway_grants ADD CONSTRAINT heat_gateway_grants_account_id_fkey
    FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE CASCADE;
CREATE INDEX heat_gateway_grants_expiry ON heat_gateway_grants(expires_at);
GRANT DELETE ON heat_gateway_grants TO wsl_platform;
