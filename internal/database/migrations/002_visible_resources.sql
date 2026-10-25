ALTER TABLE native_groups ENABLE ROW LEVEL SECURITY;
ALTER TABLE native_groups FORCE ROW LEVEL SECURITY;
CREATE POLICY visible_groups ON native_groups USING (
    current_user='wsl_platform' OR EXISTS (
        SELECT 1 FROM group_bindings b WHERE b.group_id=native_groups.id
    ) OR EXISTS (SELECT 1 FROM group_claims c WHERE c.group_id=native_groups.id)
);
ALTER TABLE accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY visible_accounts ON accounts USING (
    current_user='wsl_platform' OR EXISTS (
        SELECT 1 FROM group_bindings b
        WHERE b.primary_account_id=accounts.id OR b.ai_account_id=accounts.id
    )
);
ALTER TABLE personas ENABLE ROW LEVEL SECURITY;
ALTER TABLE personas FORCE ROW LEVEL SECURITY;
CREATE POLICY visible_personas ON personas USING (
    current_user='wsl_platform' OR EXISTS (
        SELECT 1 FROM group_bindings b
        WHERE b.primary_account_id=personas.account_id OR b.ai_account_id=personas.account_id
    )
);
GRANT SELECT ON native_groups, personas TO wsl_tenant;
GRANT SELECT (id,name,native_uid,connection_enabled,testing_enabled,runtime_state,epoch,version,created_at)
    ON accounts TO wsl_tenant;
