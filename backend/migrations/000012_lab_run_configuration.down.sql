ALTER TABLE core.lab_runs
    DROP COLUMN IF EXISTS instances,
    DROP COLUMN IF EXISTS resources;

DROP TABLE IF EXISTS core.lab_definitions;
