-- Bounded recent message windows and old, unprocessed work per account.
CREATE INDEX inbox_account_sample ON inbox(account_id,persisted_at DESC);
CREATE INDEX inbox_account_backlog ON inbox(account_id,persisted_at)
    WHERE state IN ('pending','unresolved','quarantined');
CREATE INDEX operations_account_backlog ON operations(account_id,created_at)
    WHERE status IN ('queued','running','unknown');
CREATE INDEX operations_account_failures ON operations(account_id,updated_at)
    WHERE status='failed';
