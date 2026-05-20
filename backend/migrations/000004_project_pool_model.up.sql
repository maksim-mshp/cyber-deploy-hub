CREATE TABLE IF NOT EXISTS project_pool.domains (
    domain_id text PRIMARY KEY,
    course_id text NOT NULL UNIQUE,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE project_pool.ki_projects
    ALTER COLUMN state SET DEFAULT 'FREE';

CREATE UNIQUE INDEX IF NOT EXISTS project_pool_ki_projects_domain_name_idx
    ON project_pool.ki_projects (domain_id, name);

CREATE TABLE IF NOT EXISTS project_pool.allocations (
    lab_run_id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES project_pool.ki_projects(id),
    student_id text NOT NULL,
    course_id text NOT NULL,
    lab_id text NOT NULL,
    state text NOT NULL,
    allocated_at timestamptz NOT NULL DEFAULT now(),
    released_at timestamptz,
    release_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS project_pool_allocations_active_student_course_idx
    ON project_pool.allocations (student_id, course_id)
    WHERE state = 'ACTIVE';

CREATE INDEX IF NOT EXISTS project_pool_allocations_project_state_idx
    ON project_pool.allocations (project_id, state);

CREATE TABLE IF NOT EXISTS project_pool.state_history (
    id bigserial PRIMARY KEY,
    project_id uuid REFERENCES project_pool.ki_projects(id),
    lab_run_id uuid,
    from_state text,
    to_state text NOT NULL,
    reason text,
    message_id uuid,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS project_pool_state_history_project_created_idx
    ON project_pool.state_history (project_id, created_at);
