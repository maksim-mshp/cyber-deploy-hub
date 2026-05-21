CREATE TABLE IF NOT EXISTS core.lab_definitions (
    course_id text NOT NULL,
    lab_id text NOT NULL,
    title text NOT NULL,
    description text NOT NULL DEFAULT '',
    enabled boolean NOT NULL DEFAULT true,
    resources jsonb NOT NULL,
    instances jsonb NOT NULL,
    updated_by text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (course_id, lab_id)
);

CREATE INDEX IF NOT EXISTS core_lab_definitions_enabled_idx
    ON core.lab_definitions (enabled, course_id, lab_id);

INSERT INTO core.lab_definitions (
    course_id,
    lab_id,
    title,
    description,
    enabled,
    resources,
    instances,
    updated_by
)
VALUES (
    'course-3',
    'lab-3-storage',
    'Лабораторная 3',
    'OpenStack стенд из пяти виртуальных машин для работы с инфраструктурой хранения.',
    true,
    '{"vcpu":9,"ram_mib":16384,"disk_gib":214}'::jsonb,
    '[
        {"name":"L-MS","image_id":"abfc9f03-13de-41ab-b75c-eb8b41c20a02","flavor_id":"101","fixed_ip":"10.0.0.10","disk_gib":24},
        {"name":"L-NFS","image_id":"10f6f24d-3398-4a8d-820d-7da5f34cf5ee","flavor_id":"f0a74551-ec6e-42cc-96e7-6403989d17ea","fixed_ip":"10.0.0.70","disk_gib":70},
        {"name":"L-PGSQL","image_id":"53ec46af-b200-4592-8cdc-57b1bad94939","flavor_id":"f0a74551-ec6e-42cc-96e7-6403989d17ea","fixed_ip":"10.0.0.55","disk_gib":15},
        {"name":"V-HYPERV","image_id":"3a0441e9-92b1-44f0-a30c-c1d87a2fec41","flavor_id":"103","fixed_ip":"10.0.0.65","disk_gib":45},
        {"name":"W-DC","image_id":"6dc0a314-9fe6-4ff7-8733-1060a4fbaeb5","flavor_id":"102","fixed_ip":"10.0.0.5","disk_gib":60}
    ]'::jsonb,
    'migration'
)
ON CONFLICT (course_id, lab_id) DO NOTHING;

ALTER TABLE core.lab_runs
    ADD COLUMN IF NOT EXISTS resources jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS instances jsonb NOT NULL DEFAULT '[]'::jsonb;
