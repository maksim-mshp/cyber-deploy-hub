ALTER TABLE core.lab_definitions
    ADD COLUMN IF NOT EXISTS check_profile jsonb NOT NULL DEFAULT '{}'::jsonb;
