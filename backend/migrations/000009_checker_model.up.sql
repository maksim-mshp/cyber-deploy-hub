CREATE TABLE IF NOT EXISTS checker.profiles (
    profile_id text PRIMARY KEY,
    name text NOT NULL,
    ssh_user text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS checker.profile_steps (
    id bigserial PRIMARY KEY,
    profile_id text NOT NULL REFERENCES checker.profiles(profile_id) ON DELETE CASCADE,
    sequence integer NOT NULL,
    name text NOT NULL,
    type text NOT NULL,
    package_name text,
    path text,
    contains text,
    service_name text,
    port integer,
    command text,
    expected_exit_code integer NOT NULL DEFAULT 0,
    timeout_seconds integer NOT NULL DEFAULT 15,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (profile_id, sequence)
);

CREATE TABLE IF NOT EXISTS checker.runs (
    id uuid PRIMARY KEY,
    lab_run_id uuid NOT NULL,
    profile_id text NOT NULL,
    state text NOT NULL,
    passed boolean,
    error_code text,
    error_message text,
    started_at timestamptz NOT NULL,
    finished_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS checker_runs_lab_finished_idx
    ON checker.runs (lab_run_id, finished_at DESC);

CREATE TABLE IF NOT EXISTS checker.step_results (
    id bigserial PRIMARY KEY,
    run_id uuid NOT NULL REFERENCES checker.runs(id) ON DELETE CASCADE,
    sequence integer NOT NULL,
    name text NOT NULL,
    type text NOT NULL,
    passed boolean NOT NULL,
    exit_code integer NOT NULL,
    message text,
    stdout_tail text,
    stderr_tail text,
    started_at timestamptz NOT NULL,
    finished_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (run_id, sequence)
);
