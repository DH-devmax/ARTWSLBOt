ALTER TABLE assistant_sessions
    ADD COLUMN title_sealed bytea,
    ADD COLUMN title_authorization_hash text;

ALTER TABLE assistant_sessions ADD CONSTRAINT assistant_title_pair
    CHECK ((title_sealed IS NULL) = (title_authorization_hash IS NULL));
