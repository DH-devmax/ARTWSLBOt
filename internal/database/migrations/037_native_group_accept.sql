-- Self acceptance reuses the sealed operation ledger and bounded test grants.
ALTER TABLE native_setting_operations DROP CONSTRAINT native_setting_operations_kind_check;
ALTER TABLE native_setting_operations ADD CONSTRAINT native_setting_operations_kind_check CHECK(kind IN
    ('nickname','member_nickname','search','friend_policy','group_policy','audio_video','avatar',
     'password_sms','password_verify','password','phone_old_sms','phone_old_verify','phone_new_sms','phone',
     'friend_approve','friend_reject','group_invite','group_leave','group_accept',
     'mute_member','unmute_member','mute_group','unmute_group','kick_member','publish_notice'));
