-- One container path holds one volume. The container service and the Compose
-- parser reject a duplicate first; this is the backstop.
ALTER TABLE volume_mounts
    ADD CONSTRAINT volume_mounts_container_mount_path_unique UNIQUE (container_id, mount_path);
