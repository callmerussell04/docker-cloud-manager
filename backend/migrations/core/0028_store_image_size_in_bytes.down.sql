ALTER TABLE images ADD COLUMN size_mb INTEGER NOT NULL DEFAULT 0 CHECK (size_mb >= 0);

-- Sizes are rounded up to whole megabytes.
UPDATE images SET size_mb = ((size_bytes + 1048575) / 1048576)::integer;

ALTER TABLE images
    ALTER COLUMN size_mb DROP DEFAULT,
    DROP COLUMN size_bytes;
