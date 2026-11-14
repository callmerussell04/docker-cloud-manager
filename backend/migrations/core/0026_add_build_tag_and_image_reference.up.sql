-- A build records the user tag it was queued for, so it still names it after
-- its image record is deleted (a failed or canceled build, a replaced or
-- deleted image).
ALTER TABLE builds ADD COLUMN tag TEXT NOT NULL DEFAULT '';

UPDATE builds b
SET tag = i.tag
FROM images i
WHERE i.id = b.image_id;

ALTER TABLE builds ALTER COLUMN tag DROP DEFAULT;

-- The image reference is a real foreign key: it is cleared, never left
-- dangling, when the image record goes.
ALTER TABLE builds ALTER COLUMN image_id DROP NOT NULL;

UPDATE builds b
SET image_id = NULL
WHERE NOT EXISTS (SELECT 1 FROM images i WHERE i.id = b.image_id);

ALTER TABLE builds
    ADD CONSTRAINT builds_image_id_fkey
        FOREIGN KEY (image_id) REFERENCES images(id) ON DELETE SET NULL;
