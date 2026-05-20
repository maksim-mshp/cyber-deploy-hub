DROP TABLE IF EXISTS project_pool.state_history;
DROP TABLE IF EXISTS project_pool.allocations;
DROP INDEX IF EXISTS project_pool.project_pool_ki_projects_domain_name_idx;

ALTER TABLE project_pool.ki_projects
    ALTER COLUMN state DROP DEFAULT;

DROP TABLE IF EXISTS project_pool.domains;
