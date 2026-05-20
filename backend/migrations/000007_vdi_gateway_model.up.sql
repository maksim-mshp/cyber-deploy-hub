CREATE TABLE IF NOT EXISTS vdi_gateway.access_tokens (
    token_hash text PRIMARY KEY,
    lab_run_id uuid NOT NULL,
    student_id text NOT NULL,
    project_id text NOT NULL,
    state text NOT NULL CHECK (state IN ('ACTIVE', 'REVOKED', 'EXPIRED')),
    expires_at timestamptz NOT NULL,
    issued_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    revoke_reason text,
    last_opened_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS vdi_gateway_access_tokens_lab_state_idx
    ON vdi_gateway.access_tokens (lab_run_id, state);

CREATE INDEX IF NOT EXISTS vdi_gateway_access_tokens_expires_idx
    ON vdi_gateway.access_tokens (expires_at)
    WHERE state = 'ACTIVE';

CREATE TABLE IF NOT EXISTS vdi_gateway.session_opens (
    id bigserial PRIMARY KEY,
    token_hash text NOT NULL REFERENCES vdi_gateway.access_tokens(token_hash) ON DELETE CASCADE,
    lab_run_id uuid NOT NULL,
    opened_at timestamptz NOT NULL DEFAULT now(),
    remote_addr text,
    user_agent text
);

CREATE INDEX IF NOT EXISTS vdi_gateway_session_opens_lab_idx
    ON vdi_gateway.session_opens (lab_run_id, opened_at DESC);

CREATE TABLE IF NOT EXISTS vdi_gateway.access_failures (
    id bigserial PRIMARY KEY,
    lab_run_id uuid,
    student_id text,
    project_id text,
    reason text NOT NULL,
    message_id uuid NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);
