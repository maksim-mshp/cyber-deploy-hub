ALTER TABLE cloud_adapter.deployments
    ADD COLUMN IF NOT EXISTS network_id text,
    ADD COLUMN IF NOT EXISTS subnet_id text;

