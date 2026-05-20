DROP TABLE IF EXISTS core.lab_run_events;

ALTER TABLE core.lab_runs
    DROP COLUMN IF EXISTS vdi_access_url,
    DROP COLUMN IF EXISTS project_id;
