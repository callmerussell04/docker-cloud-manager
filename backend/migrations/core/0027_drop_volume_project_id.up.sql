-- project_volumes is the only record of which projects a volume belongs to.
-- Every volume created in a project has had its link row since migration 0014;
-- any that lacks one gets it before the column goes.
INSERT INTO project_volumes (project_id, volume_id)
SELECT project_id, id
FROM volumes
WHERE project_id IS NOT NULL
ON CONFLICT DO NOTHING;

DROP INDEX idx_volumes_project_id;

ALTER TABLE volumes DROP COLUMN project_id;
