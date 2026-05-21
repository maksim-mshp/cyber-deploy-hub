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
VALUES
(
    'course-3',
    'lab-1-debian',
    'Лабораторная 1',
    'Минимальный стенд с одной Debian VM для вводной лабораторной работы.',
    true,
    '{"vcpu":1,"ram_mib":2048,"disk_gib":24}'::jsonb,
    '[
        {"name":"LAB1-DEBIAN","image_id":"abfc9f03-13de-41ab-b75c-eb8b41c20a02","flavor_id":"101","fixed_ip":"10.0.0.10","disk_gib":24}
    ]'::jsonb,
    'migration'
),
(
    'course-3',
    'lab-2-debian',
    'Лабораторная 2',
    'Минимальный стенд с одной Debian VM для второй лабораторной работы.',
    true,
    '{"vcpu":1,"ram_mib":2048,"disk_gib":24}'::jsonb,
    '[
        {"name":"LAB2-DEBIAN","image_id":"abfc9f03-13de-41ab-b75c-eb8b41c20a02","flavor_id":"101","fixed_ip":"10.0.0.10","disk_gib":24}
    ]'::jsonb,
    'migration'
)
ON CONFLICT (course_id, lab_id) DO UPDATE
SET title = EXCLUDED.title,
    description = EXCLUDED.description,
    enabled = EXCLUDED.enabled,
    resources = EXCLUDED.resources,
    instances = EXCLUDED.instances,
    updated_by = EXCLUDED.updated_by,
    updated_at = now();
