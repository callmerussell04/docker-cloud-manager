ALTER TABLE volumes
    DROP COLUMN IF EXISTS driver_opts,
    DROP COLUMN IF EXISTS driver;
