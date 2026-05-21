CREATE TABLE IF NOT EXISTS core.lab_definitions (
    course_id text NOT NULL,
    lab_id text NOT NULL,
    title text NOT NULL,
    description text NOT NULL DEFAULT '',
    enabled boolean NOT NULL DEFAULT true,
    resources jsonb NOT NULL,
    instances jsonb NOT NULL,
    updated_by text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (course_id, lab_id)
);

CREATE INDEX IF NOT EXISTS core_lab_definitions_enabled_idx
    ON core.lab_definitions (enabled, course_id, lab_id);

ALTER TABLE core.lab_runs
    ADD COLUMN IF NOT EXISTS resources jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS instances jsonb NOT NULL DEFAULT '[]'::jsonb;
