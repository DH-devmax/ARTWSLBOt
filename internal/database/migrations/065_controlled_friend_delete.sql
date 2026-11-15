-- Extend the existing ledger; deletion is never a customer or model operation.
DO $$
DECLARE previous text;
BEGIN
    SELECT pg_get_expr(conbin,conrelid) INTO STRICT previous
      FROM pg_constraint
      WHERE conrelid='native_setting_operations'::regclass
        AND conname='native_setting_operations_kind_check';
    ALTER TABLE native_setting_operations DROP CONSTRAINT native_setting_operations_kind_check;
    EXECUTE format('ALTER TABLE native_setting_operations ADD CONSTRAINT native_setting_operations_kind_check
      CHECK ((%s) OR kind=''friend_delete'')', previous);
END $$;

ALTER TABLE native_setting_operations ADD CONSTRAINT native_friend_delete_scope
    CHECK(kind<>'friend_delete' OR (test_window_id IS NOT NULL AND group_id IS NOT NULL));
ALTER TABLE test_windows ADD CONSTRAINT test_window_friend_delete_scope
    CHECK(NOT 'friend_delete'=ANY(actions) OR (
        actions=ARRAY['friend_delete']::text[] AND admission_mode='native_debug'
        AND cardinality(account_ids)=1 AND cardinality(target_ids)=1 AND budget=1));
