CREATE TABLE projects (
                          id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                          owner_id UUID NOT NULL,
                          name VARCHAR(100) NOT NULL,
                          created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
                          status VARCHAR(20) DEFAULT 'pending',
                          error_message TEXT
);

ALTER TABLE containers ADD COLUMN project_id UUID REFERENCES projects(id) ON DELETE CASCADE;
ALTER TABLE volumes ADD COLUMN project_id UUID REFERENCES projects(id) ON DELETE CASCADE;
