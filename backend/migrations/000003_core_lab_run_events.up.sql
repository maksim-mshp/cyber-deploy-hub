ALTER TABLE core.lab_runs
    ADD COLUMN IF NOT EXISTS project_id text,
    ADD COLUMN IF NOT EXISTS vdi_access_url text;

CREATE TABLE IF NOT EXISTS core.lab_run_events (
    id bigserial PRIMARY KEY,
    lab_run_id uuid NOT NULL REFERENCES core.lab_runs(id),
    state text NOT NULL,
    message_id uuid,
    message_type text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS core_lab_run_events_lab_run_created_idx
    ON core.lab_run_events (lab_run_id, created_at);
