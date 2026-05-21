CREATE TABLE IF NOT EXISTS lifecycle.timers (
    id bigserial PRIMARY KEY,
    lab_run_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('CLEANUP')),
    state text NOT NULL CHECK (state IN ('SCHEDULED', 'FIRED', 'CANCELLED')),
    due_at timestamptz NOT NULL,
    reason text,
    created_by_message_id uuid,
    fired_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (lab_run_id, kind)
);

CREATE INDEX IF NOT EXISTS lifecycle_timers_due_idx
    ON lifecycle.timers (state, due_at)
    WHERE state = 'SCHEDULED';

CREATE TABLE IF NOT EXISTS lifecycle.timer_history (
    id bigserial PRIMARY KEY,
    timer_id bigint REFERENCES lifecycle.timers(id) ON DELETE SET NULL,
    lab_run_id uuid NOT NULL,
    kind text NOT NULL,
    state text NOT NULL,
    due_at timestamptz,
    reason text,
    message_id uuid,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS lifecycle_timer_history_lab_idx
    ON lifecycle.timer_history (lab_run_id, created_at DESC);

CREATE TABLE IF NOT EXISTS lifecycle.settings (
    key text PRIMARY KEY,
    value jsonb NOT NULL,
    updated_by text,
    updated_at timestamptz NOT NULL DEFAULT now()
);
