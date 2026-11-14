-- The column holds the object-storage key of the build log, not a file path.
ALTER TABLE builds RENAME COLUMN log_file_path TO log_object_key;
