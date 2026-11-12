-- Core owns user quotas (ADR 0005); SSO holds identity only.
ALTER TABLE users
    DROP COLUMN quota_cpu,
    DROP COLUMN quota_ram_mb,
    DROP COLUMN quota_disk_mb;
