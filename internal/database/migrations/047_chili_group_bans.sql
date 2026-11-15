DO $$
DECLARE previous text;
BEGIN
    SELECT pg_get_expr(conbin,conrelid) INTO STRICT previous
      FROM pg_constraint
      WHERE conrelid='native_setting_operations'::regclass
        AND conname='native_setting_operations_kind_check';
    ALTER TABLE native_setting_operations DROP CONSTRAINT native_setting_operations_kind_check;
    EXECUTE format('ALTER TABLE native_setting_operations ADD CONSTRAINT native_setting_operations_kind_check
      CHECK ((%s) OR kind IN (''kick_member_ban'',''remove_group_ban''))', previous);
END $$;
