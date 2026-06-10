CREATE UNIQUE INDEX idx_containers_owner_name_unique
    ON containers (owner_id, name);

CREATE UNIQUE INDEX idx_projects_owner_name_unique
    ON projects (owner_id, name);
