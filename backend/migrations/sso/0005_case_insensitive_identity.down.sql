-- Lowercased values are not restored to their original case.
DROP INDEX users_email_lower_unique;
DROP INDEX users_username_lower_unique;

ALTER TABLE users
    ADD CONSTRAINT users_username_key UNIQUE (username),
    ADD CONSTRAINT users_email_key UNIQUE (email);
