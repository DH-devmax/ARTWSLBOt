-- Private sends retain the authorized test group as their grant, while their
-- destination is an independent, sealed conversation/peer. Existing group
-- operations and receipts keep their exact meaning.
ALTER TABLE operations ADD COLUMN conversation_key text NOT NULL DEFAULT ''
    CHECK(conversation_key='' OR conversation_key ~ '^[a-f0-9]{64}$');
ALTER TABLE native_media_uploads ADD COLUMN conversation_key text NOT NULL DEFAULT ''
    CHECK(conversation_key='' OR conversation_key ~ '^[a-f0-9]{64}$');
ALTER TABLE native_text_routes ADD COLUMN private_peer text NOT NULL DEFAULT ''
    CHECK(length(private_peer)<=128);
CREATE INDEX native_private_operation_history ON operations(account_id,actor_id,conversation_key,created_at DESC)
    WHERE conversation_key<>'';
CREATE POLICY private_operation_scope ON operations AS RESTRICTIVE
    USING(current_user='wsl_platform' OR conversation_key='')
    WITH CHECK(current_user='wsl_platform' OR conversation_key='');
