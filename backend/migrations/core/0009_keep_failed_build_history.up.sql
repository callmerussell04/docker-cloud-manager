ALTER TABLE builds ADD COLUMN owner_id UUID;

ALTER TABLE builds ALTER COLUMN status TYPE VARCHAR(32);

UPDATE builds b
SET owner_id = i.owner_id
FROM images i
WHERE b.image_id = i.id;

ALTER TABLE builds ALTER COLUMN owner_id SET NOT NULL;

ALTER TABLE builds DROP CONSTRAINT IF EXISTS builds_image_id_fkey;

CREATE INDEX idx_builds_owner_id_started_at ON builds (owner_id, started_at DESC);
