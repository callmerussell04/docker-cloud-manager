-- An image's Registry repository (imageref.CustomRepositoryName: the owner ID
-- and the name part of the user tag, lowercased) is defined once here and
-- indexed, so Registry authorization and cleanup look it up instead of
-- computing it for every image row.
ALTER TABLE images
    ADD COLUMN repository TEXT NOT NULL
        GENERATED ALWAYS AS (lower(owner_id::text || '_' || split_part(tag, ':', 1))) STORED;

CREATE INDEX idx_images_repository
    ON images (repository);
