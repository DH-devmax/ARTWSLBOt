ALTER TABLE test_windows
    ADD COLUMN friend_application_id text NOT NULL DEFAULT '';

ALTER TABLE test_windows
    ADD CONSTRAINT test_window_friend_approve_scope
    CHECK(NOT 'friend_approve'=ANY(actions) OR (
        actions=ARRAY['friend_approve']::text[]
        AND admission_mode='native_debug'
        AND cardinality(account_ids)=1
        AND cardinality(target_ids)=1
        AND length(friend_application_id) BETWEEN 1 AND 200
        AND budget=1
    ));
