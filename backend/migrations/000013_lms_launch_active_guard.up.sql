CREATE INDEX IF NOT EXISTS lms_gateway_launches_student_created_idx
    ON lms_gateway.launches (local_student_id, created_at DESC);
