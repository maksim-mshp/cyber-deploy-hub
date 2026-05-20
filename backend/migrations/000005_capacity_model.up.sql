CREATE TABLE IF NOT EXISTS capacity.snapshots (
    id bigserial PRIMARY KEY,
    source text NOT NULL,
    vcpus_total integer NOT NULL,
    vcpus_free integer NOT NULL,
    ram_mib_total bigint NOT NULL,
    ram_mib_free bigint NOT NULL,
    storage_gib_total bigint NOT NULL,
    storage_gib_used bigint NOT NULL,
    raw_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS capacity.decisions (
    id bigserial PRIMARY KEY,
    lab_run_id uuid NOT NULL,
    project_id text NOT NULL,
    message_id uuid NOT NULL UNIQUE,
    snapshot_id bigint NOT NULL REFERENCES capacity.snapshots(id),
    approved boolean NOT NULL,
    requested_vcpu integer NOT NULL,
    requested_ram_mib integer NOT NULL,
    requested_disk_gib bigint NOT NULL,
    predicted_cpu_percent numeric(6,2) NOT NULL,
    predicted_ram_percent numeric(6,2) NOT NULL,
    predicted_storage_percent numeric(6,2) NOT NULL,
    threshold_percent numeric(6,2) NOT NULL,
    reason text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS capacity_decisions_lab_created_idx
    ON capacity.decisions (lab_run_id, created_at);

CREATE TABLE IF NOT EXISTS capacity.reservations (
    lab_run_id uuid PRIMARY KEY,
    project_id text NOT NULL,
    vcpu integer NOT NULL,
    ram_mib integer NOT NULL,
    disk_gib bigint NOT NULL,
    state text NOT NULL,
    reserved_at timestamptz NOT NULL DEFAULT now(),
    released_at timestamptz,
    release_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS capacity_reservations_state_idx
    ON capacity.reservations (state, reserved_at);
