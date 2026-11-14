-- A username or an email identifies one user whatever its case. SSO stores
-- both lowercased; the unique indexes are the backstop. The migration fails if
-- two existing users differ only by case.
UPDATE users
SET username = lower(username), email = lower(email)
WHERE username <> lower(username) OR email <> lower(email);

ALTER TABLE users
    DROP CONSTRAINT users_username_key,
    DROP CONSTRAINT users_email_key;

CREATE UNIQUE INDEX users_username_lower_unique ON users (lower(username));
CREATE UNIQUE INDEX users_email_lower_unique ON users (lower(email));
