ALTER TABLE inbox ADD COLUMN binding_version bigint;
ALTER TABLE inbox ADD CONSTRAINT positive_binding_version CHECK (binding_version IS NULL OR binding_version>0);
