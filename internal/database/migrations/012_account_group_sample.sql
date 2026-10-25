-- Quiet tenant groups must not scan a shared account's other message history
-- when looking up their latest message.
CREATE INDEX inbox_account_group_sample ON inbox(account_id,group_id,persisted_at DESC);
