ALTER TABLE activities ADD CONSTRAINT activities_tenant_identity UNIQUE (tenant_id,id);
ALTER TABLE activity_members ADD CONSTRAINT activity_member_scope
    FOREIGN KEY (tenant_id,activity_id) REFERENCES activities(tenant_id,id);

REVOKE ALL ON schema_migrations FROM wsl_platform;
REVOKE UPDATE, DELETE ON ledger_entries FROM wsl_platform;

ALTER TABLE test_windows ADD CONSTRAINT test_deadline_positive CHECK (expires_at>created_at);
ALTER TABLE operations ADD CONSTRAINT operation_scope
    FOREIGN KEY (tenant_id,group_id) REFERENCES group_bindings(tenant_id,group_id);
