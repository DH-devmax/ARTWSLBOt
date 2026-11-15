-- Add manual Chili profile operations without changing grants or activation.
DO $$
DECLARE previous text;
BEGIN
    SELECT pg_get_expr(conbin, conrelid) INTO STRICT previous
      FROM pg_constraint WHERE conrelid='native_setting_operations'::regclass
      AND conname='native_setting_operations_kind_check';
    ALTER TABLE native_setting_operations DROP CONSTRAINT native_setting_operations_kind_check;
    EXECUTE format('ALTER TABLE native_setting_operations ADD CONSTRAINT native_setting_operations_kind_check CHECK
      ((%s) OR kind IN (''profile_gender'',''profile_birth'',''profile_signature'',''push_notifications''))', previous);
END $$;
