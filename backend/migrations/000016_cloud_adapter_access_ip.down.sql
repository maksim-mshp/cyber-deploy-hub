DROP INDEX IF EXISTS cloud_adapter.cloud_adapter_instances_floating_ip_idx;

ALTER TABLE cloud_adapter.instances
    DROP COLUMN IF EXISTS floating_ip_id,
    DROP COLUMN IF EXISTS access_ip;
