-- Image size is stored in bytes, as the Registry reports it, so reserved disk
-- is exact. Existing sizes were whole megabytes.
ALTER TABLE images ADD COLUMN size_bytes BIGINT NOT NULL DEFAULT 0 CHECK (size_bytes >= 0);

UPDATE images SET size_bytes = size_mb::bigint * 1048576;

ALTER TABLE images
    ALTER COLUMN size_bytes DROP DEFAULT,
    DROP COLUMN size_mb;
