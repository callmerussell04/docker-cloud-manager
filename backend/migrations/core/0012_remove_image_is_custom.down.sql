ALTER TABLE images ADD COLUMN is_custom BOOLEAN NOT NULL DEFAULT true;

DROP INDEX IF EXISTS idx_images_owner_id_tag;

CREATE UNIQUE INDEX idx_images_owner_id_tag_custom ON images (owner_id, tag) WHERE is_custom = true;
