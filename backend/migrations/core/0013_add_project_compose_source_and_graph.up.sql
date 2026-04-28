CREATE TABLE project_services (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    container_id UUID NOT NULL REFERENCES containers(id) ON DELETE CASCADE,
    service_name VARCHAR(100) NOT NULL,
    start_order INTEGER NOT NULL,
    PRIMARY KEY (project_id, container_id),
    UNIQUE (project_id, service_name),
    UNIQUE (project_id, start_order)
);

CREATE TABLE project_service_dependencies (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    container_id UUID NOT NULL REFERENCES containers(id) ON DELETE CASCADE,
    depends_on_container_id UUID NOT NULL REFERENCES containers(id) ON DELETE CASCADE,
    condition VARCHAR(64) NOT NULL CHECK (condition IN ('service_started', 'service_healthy', 'service_completed_successfully')),
    PRIMARY KEY (project_id, container_id, depends_on_container_id)
);

CREATE INDEX idx_project_services_project_order
    ON project_services (project_id, start_order);

CREATE INDEX idx_project_service_dependencies_project
    ON project_service_dependencies (project_id, container_id);
