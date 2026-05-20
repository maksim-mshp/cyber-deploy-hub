CREATE SCHEMA IF NOT EXISTS api_gateway;
CREATE SCHEMA IF NOT EXISTS core;
CREATE SCHEMA IF NOT EXISTS project_pool;

CREATE TABLE IF NOT EXISTS api_gateway.outbox (
    id bigserial PRIMARY KEY,
    message_id uuid NOT NULL UNIQUE,
    message_kind text NOT NULL,
    subject text NOT NULL,
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'PENDING',
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    last_error text
);

CREATE INDEX IF NOT EXISTS api_gateway_outbox_pending_idx
    ON api_gateway.outbox (status, available_at, created_at);

CREATE TABLE IF NOT EXISTS api_gateway.inbox (
    id bigserial PRIMARY KEY,
    message_id uuid NOT NULL UNIQUE,
    message_kind text NOT NULL,
    subject text NOT NULL,
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'RECEIVED',
    received_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz
);

CREATE TABLE IF NOT EXISTS core.lab_runs (
    id uuid PRIMARY KEY,
    student_id text NOT NULL,
    course_id text NOT NULL,
    lab_id text NOT NULL,
    state text NOT NULL,
    failure_code text,
    failure_message text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS core_lab_runs_student_course_lab_idx
    ON core.lab_runs (student_id, course_id, lab_id);

CREATE TABLE IF NOT EXISTS core.saga_instances (
    id uuid PRIMARY KEY,
    lab_run_id uuid NOT NULL REFERENCES core.lab_runs(id),
    saga_type text NOT NULL,
    state text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS core.saga_steps (
    id bigserial PRIMARY KEY,
    saga_id uuid NOT NULL REFERENCES core.saga_instances(id),
    step_name text NOT NULL,
    state text NOT NULL,
    message_id uuid,
    error_message text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS core.outbox (LIKE api_gateway.outbox INCLUDING ALL);
CREATE TABLE IF NOT EXISTS core.inbox (LIKE api_gateway.inbox INCLUDING ALL);

CREATE TABLE IF NOT EXISTS project_pool.ki_projects (
    id uuid PRIMARY KEY,
    domain_id text NOT NULL,
    name text NOT NULL,
    state text NOT NULL,
    current_lab_run_id uuid,
    reserved_by_student_id text,
    reserved_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS project_pool_ki_projects_state_idx
    ON project_pool.ki_projects (state, updated_at);

CREATE TABLE IF NOT EXISTS project_pool.outbox (LIKE api_gateway.outbox INCLUDING ALL);
CREATE TABLE IF NOT EXISTS project_pool.inbox (LIKE api_gateway.inbox INCLUDING ALL);
