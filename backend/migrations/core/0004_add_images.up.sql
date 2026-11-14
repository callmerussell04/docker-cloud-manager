CREATE TABLE images (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL,
    -- The user-facing tag lives only here. Each record owns its Registry tag,
    -- so a rebuild never pushes over the image available under the same user
    -- tag.
    tag VARCHAR(100) NOT NULL,
    registry_tag TEXT NOT NULL CONSTRAINT images_registry_tag_not_empty CHECK (registry_tag <> ''),
    -- The Registry repository (imageref.CustomRepositoryName: the owner ID and
    -- the name part of the user tag, lowercased), defined once here so
    -- Registry authorization and cleanup look it up by index.
    repository TEXT NOT NULL
        GENERATED ALWAYS AS (lower(owner_id::text || '_' || split_part(tag, ':', 1))) STORED,
    digest TEXT,
    -- The size the Registry reports, counted against the owner's disk quota.
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    status VARCHAR(20) NOT NULL DEFAULT 'available' CHECK (status IN (
        'building', 'available', 'deleting', 'missing', 'error')),
    last_observed_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- An owner has at most one record of a tag being built and one that is not.
CREATE UNIQUE INDEX idx_images_owner_tag_available_unique
    ON images (owner_id, tag)
    WHERE status != 'building';

CREATE UNIQUE INDEX idx_images_owner_tag_building_unique
    ON images (owner_id, tag)
    WHERE status = 'building';

CREATE INDEX idx_images_owner_created_at
    ON images (owner_id, created_at DESC);

CREATE INDEX idx_images_repository
    ON images (repository);
