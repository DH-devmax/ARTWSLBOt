ALTER TABLE monitor_incidents DROP CONSTRAINT monitor_incidents_component_check;
ALTER TABLE monitor_incidents ADD CONSTRAINT monitor_incidents_component_check
    CHECK (component IN ('simon','database','receiver','decoder','http','backlog','web','heatbot'));
