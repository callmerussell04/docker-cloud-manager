DROP INDEX IF EXISTS idx_images_owner_id_tag;

CREATE UNIQUE INDEX idx_images_owner_tag_available_unique
    ON images (owner_id, tag)
    WHERE status != 'building';

CREATE UNIQUE INDEX idx_images_owner_tag_building_unique
    ON images (owner_id, tag)
    WHERE status = 'building';
