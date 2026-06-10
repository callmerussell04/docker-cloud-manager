DROP INDEX IF EXISTS idx_images_owner_tag_building_unique;
DROP INDEX IF EXISTS idx_images_owner_tag_available_unique;

CREATE UNIQUE INDEX idx_images_owner_id_tag
    ON images (owner_id, tag);
