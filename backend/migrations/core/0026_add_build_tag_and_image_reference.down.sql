ALTER TABLE builds DROP CONSTRAINT builds_image_id_fkey;

-- Cleared references become dangling IDs again, as they were before.
UPDATE builds SET image_id = gen_random_uuid() WHERE image_id IS NULL;

ALTER TABLE builds
    ALTER COLUMN image_id SET NOT NULL,
    DROP COLUMN tag;
