-- Deleted orphan building rows are not restored. Images built after the up
-- migration live in the Registry under their image ID; once registry_tag is
-- dropped, code that derives the tag from the user's version cannot pull or
-- delete them.
ALTER TABLE images
    DROP CONSTRAINT images_registry_tag_not_empty,
    DROP COLUMN digest,
    DROP COLUMN registry_tag;
