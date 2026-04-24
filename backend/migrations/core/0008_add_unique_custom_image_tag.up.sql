CREATE UNIQUE INDEX idx_images_owner_id_tag_custom ON images (owner_id, tag) WHERE is_custom = true;
