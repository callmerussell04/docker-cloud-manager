-- Each image record owns its Registry tag, so a rebuild never pushes over the
-- image that is available under the same user tag. The user tag lives only in
-- Postgres.
ALTER TABLE images
    ADD COLUMN registry_tag TEXT,
    ADD COLUMN digest TEXT;

-- Building rows without a pending or running build are orphans of deleted
-- builds; they block their tag forever.
DELETE FROM images i
WHERE i.status = 'building'
  AND NOT EXISTS (
      SELECT 1 FROM builds b
      WHERE b.image_id = i.id AND b.status IN ('pending', 'running')
  );

-- A build not yet claimed pushes to its own image ID.
UPDATE images i
SET registry_tag = i.id::text
WHERE EXISTS (
    SELECT 1 FROM builds b WHERE b.image_id = i.id AND b.status = 'pending'
);

-- Every other row keeps the tag it was pushed under: the version part of its
-- user tag (imageref.ParseTag), "latest" when absent.
UPDATE images
SET registry_tag = COALESCE(NULLIF(substring(tag FROM position(':' IN tag) + 1), ''), 'latest')
WHERE registry_tag IS NULL AND position(':' IN tag) > 0;

UPDATE images
SET registry_tag = 'latest'
WHERE registry_tag IS NULL;

ALTER TABLE images
    ALTER COLUMN registry_tag SET NOT NULL,
    ADD CONSTRAINT images_registry_tag_not_empty CHECK (registry_tag <> '');
