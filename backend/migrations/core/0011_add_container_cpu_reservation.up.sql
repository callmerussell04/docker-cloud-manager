ALTER TABLE containers
    ADD COLUMN base_cpu_millicores BIGINT NOT NULL DEFAULT 250 CHECK (base_cpu_millicores >= 0);

