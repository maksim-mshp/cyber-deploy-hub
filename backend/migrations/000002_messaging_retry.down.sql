ALTER TABLE audit.inbox
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS attempts;

ALTER TABLE settings.inbox
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS attempts;

ALTER TABLE checker.inbox
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS attempts;

ALTER TABLE lifecycle.inbox
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS attempts;

ALTER TABLE vdi_gateway.inbox
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS attempts;

ALTER TABLE cloud_adapter.inbox
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS attempts;

ALTER TABLE capacity.inbox
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS attempts;

ALTER TABLE project_pool.inbox
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS attempts;

ALTER TABLE lms_gateway.inbox
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS attempts;

ALTER TABLE identity.inbox
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS attempts;

ALTER TABLE core.inbox
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS attempts;

ALTER TABLE api_gateway.inbox
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS attempts;
