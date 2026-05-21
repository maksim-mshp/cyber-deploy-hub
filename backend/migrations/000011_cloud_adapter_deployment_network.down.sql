ALTER TABLE cloud_adapter.deployments
    DROP COLUMN IF EXISTS subnet_id,
    DROP COLUMN IF EXISTS network_id;

