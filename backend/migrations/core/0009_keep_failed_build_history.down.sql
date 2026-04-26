DROP INDEX IF EXISTS idx_builds_owner_id_started_at;

DELETE FROM builds b
WHERE NOT EXISTS (
    SELECT 1
    FROM images i
    WHERE i.id = b.image_id
);

ALTER TABLE builds
    ADD CONSTRAINT builds_image_id_fkey
    FOREIGN KEY (image_id)
    REFERENCES images(id)
    ON DELETE CASCADE;

UPDATE builds
SET status = 'failed'
WHERE length(status) > 20;

ALTER TABLE builds ALTER COLUMN status TYPE VARCHAR(20);

ALTER TABLE builds DROP COLUMN owner_id;
