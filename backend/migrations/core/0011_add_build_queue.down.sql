DROP INDEX IF EXISTS idx_build_queue_outbox_pending;
DROP TABLE IF EXISTS build_queue_outbox;

DROP INDEX IF EXISTS idx_builds_active_status;

ALTER TABLE builds
    DROP COLUMN IF EXISTS archive_object_key;
