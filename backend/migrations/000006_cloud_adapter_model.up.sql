CREATE TABLE IF NOT EXISTS cloud_adapter.deployments (
    lab_run_id uuid PRIMARY KEY,
    project_id text NOT NULL,
    state text NOT NULL,
    keypair_name text,
    private_key_ciphertext bytea,
    private_key_nonce bytea,
    private_key_key_id text,
    failure_reason text,
    cleaned_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS cloud_adapter_deployments_project_state_idx
    ON cloud_adapter.deployments (project_id, state);

CREATE TABLE IF NOT EXISTS cloud_adapter.instances (
    id bigserial PRIMARY KEY,
    lab_run_id uuid NOT NULL REFERENCES cloud_adapter.deployments(lab_run_id) ON DELETE CASCADE,
    name text NOT NULL,
    image_id text NOT NULL,
    flavor_id text NOT NULL,
    fixed_ip inet,
    disk_gib bigint NOT NULL,
    server_id text,
    volume_id text,
    port_id text,
    state text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (lab_run_id, name)
);

CREATE INDEX IF NOT EXISTS cloud_adapter_instances_server_idx
    ON cloud_adapter.instances (server_id)
    WHERE server_id IS NOT NULL;
