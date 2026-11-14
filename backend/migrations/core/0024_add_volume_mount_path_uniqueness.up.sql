-- One container path holds one volume. Container create rejects a duplicate
-- first; this is the backstop. The migration fails if
-- an existing container already has two volumes at one path.
ALTER TABLE volume_mounts
    ADD CONSTRAINT volume_mounts_container_mount_path_unique UNIQUE (container_id, mount_path);
