DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM images WHERE is_custom = false) THEN
        RAISE EXCEPTION 'cannot remove images.is_custom while non-custom image rows exist';
    END IF;
END $$;

DROP INDEX IF EXISTS idx_images_owner_id_tag_custom;

CREATE UNIQUE INDEX idx_images_owner_id_tag ON images (owner_id, tag);

ALTER TABLE images DROP COLUMN is_custom;
