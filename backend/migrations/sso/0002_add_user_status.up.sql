CREATE TYPE user_status AS ENUM ('active', 'deactivated');

ALTER TABLE users
    ADD COLUMN status user_status NOT NULL DEFAULT 'active';

CREATE INDEX idx_users_status ON users(status);
