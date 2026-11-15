ALTER TABLE assistant_turns ADD COLUMN history_pruned_at timestamptz;

CREATE INDEX assistant_visible_turn_history
    ON assistant_turns(session_id,created_at DESC,id DESC)
    WHERE history_pruned_at IS NULL;
