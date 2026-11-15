-- Existing grants retain package admission; only a new explicit superadmin
-- isolated grant can select native_debug. Production settings stay untouched.
ALTER TABLE test_windows ADD COLUMN admission_mode text NOT NULL DEFAULT 'package'
    CHECK(admission_mode IN ('package','native_debug'));

ALTER TABLE test_windows DROP CONSTRAINT test_windows_actions_check;
ALTER TABLE test_windows ADD CONSTRAINT test_windows_actions_check
    CHECK(cardinality(actions) BETWEEN 1 AND 32);

ALTER TABLE native_setting_operations DROP CONSTRAINT native_setting_operations_kind_check;
ALTER TABLE native_setting_operations ADD CONSTRAINT native_setting_operations_kind_check CHECK(kind IN
    ('nickname','member_nickname','search','friend_policy','group_policy','audio_video','avatar',
     'password_sms','password_verify','password','phone_old_sms','phone_old_verify','phone_new_sms','phone',
     'friend_approve','friend_reject','group_invite','group_leave','group_accept',
     'mute_member','unmute_member','mute_group','unmute_group','kick_member','publish_notice',
     'grant_admin','revoke_admin'));
