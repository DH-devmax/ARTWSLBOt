CREATE TABLE assistant_module_exposure (
 platform text PRIMARY KEY CHECK (platform IN ('wsl','lajiaoliao')),
 capabilities text[] NOT NULL DEFAULT '{}' CHECK (cardinality(capabilities)<=128),
 version bigint NOT NULL DEFAULT 1 CHECK (version>0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 updated_by uuid REFERENCES users(id)
);
INSERT INTO assistant_module_exposure(platform) VALUES ('wsl'),('lajiaoliao');
ALTER TABLE assistant_module_exposure ENABLE ROW LEVEL SECURITY;
ALTER TABLE assistant_module_exposure FORCE ROW LEVEL SECURITY;
CREATE POLICY assistant_module_exposure_platform ON assistant_module_exposure
 USING (current_user='wsl_platform') WITH CHECK (current_user='wsl_platform');
GRANT SELECT,INSERT,UPDATE,DELETE ON assistant_module_exposure TO wsl_platform;
ALTER TABLE assistant_turns ADD COLUMN module_policy_version bigint NOT NULL DEFAULT 0 CHECK(module_policy_version>=0);
