ALTER TABLE project_service_dependencies
    ADD COLUMN required BOOLEAN NOT NULL DEFAULT TRUE;
