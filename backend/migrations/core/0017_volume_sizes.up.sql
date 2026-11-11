-- Every volume has a size reserved against its owner's disk quota. Existing
-- volumes get the default 256 MiB and keep a measured (not kernel-enforced) size.
ALTER TABLE volumes ADD COLUMN size_bytes BIGINT NOT NULL DEFAULT 268435456 CHECK (size_bytes >= 1048576);
ALTER TABLE volumes ALTER COLUMN size_bytes DROP DEFAULT;
ALTER TABLE volumes ADD COLUMN size_enforced BOOLEAN NOT NULL DEFAULT FALSE;
