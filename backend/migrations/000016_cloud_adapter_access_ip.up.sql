ALTER TABLE cloud_adapter.instances
    ADD COLUMN IF NOT EXISTS access_ip inet,
    ADD COLUMN IF NOT EXISTS floating_ip_id text;

CREATE INDEX IF NOT EXISTS cloud_adapter_instances_floating_ip_idx
    ON cloud_adapter.instances (floating_ip_id)
    WHERE floating_ip_id IS NOT NULL;
