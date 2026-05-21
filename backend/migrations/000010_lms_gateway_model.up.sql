CREATE TABLE IF NOT EXISTS lms_gateway.launches (
    id uuid PRIMARY KEY,
    idempotency_key text NOT NULL UNIQUE,
    external_user_id text NOT NULL,
    external_course_id text NOT NULL,
    external_assignment_id text NOT NULL,
    external_user_login text,
    external_course_name text,
    local_student_id text NOT NULL,
    local_course_id text NOT NULL,
    local_lab_id text NOT NULL,
    lab_run_id uuid NOT NULL,
    saga_id uuid NOT NULL,
    command_id uuid NOT NULL,
    status text NOT NULL,
    request_payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS lms_gateway_launches_external_idx
    ON lms_gateway.launches (external_user_id, external_course_id, external_assignment_id);

CREATE INDEX IF NOT EXISTS lms_gateway_launches_lab_run_idx
    ON lms_gateway.launches (lab_run_id);
