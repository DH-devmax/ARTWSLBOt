ALTER TABLE assistant_sessions ADD COLUMN deleted_at timestamptz;

CREATE INDEX assistant_live_scope_history
    ON assistant_sessions(actor_id,domain,group_id,created_at DESC)
    WHERE deleted_at IS NULL;
