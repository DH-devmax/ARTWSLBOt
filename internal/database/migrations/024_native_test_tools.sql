-- Bounded tool calls reuse existing authorization windows and operation ledger.
ALTER TABLE native_setting_operations DROP CONSTRAINT native_setting_operations_kind_check;
ALTER TABLE native_setting_operations ADD CONSTRAINT native_setting_operations_kind_check CHECK(kind IN
    ('nickname','member_nickname','search','friend_policy','group_policy','audio_video','avatar',
     'password_sms','password_verify','password','phone_old_sms','phone_old_verify','phone_new_sms','phone',
     'friend_approve','friend_reject','group_invite','group_leave',
     'mute_member','unmute_member','mute_group','unmute_group','kick_member','publish_notice'));
ALTER TABLE native_setting_operations ADD COLUMN test_window_id uuid REFERENCES test_windows(id) ON DELETE SET NULL;
CREATE INDEX native_setting_test_window ON native_setting_operations(test_window_id)
    WHERE test_window_id IS NOT NULL;
ALTER TABLE test_windows DROP CONSTRAINT test_windows_actions_check;
ALTER TABLE test_windows ADD CONSTRAINT test_windows_actions_check
    CHECK (cardinality(actions) BETWEEN 1 AND 10);
