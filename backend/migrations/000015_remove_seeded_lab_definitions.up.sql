DELETE FROM core.lab_definitions
WHERE course_id = 'course-3'
  AND updated_by = 'migration'
  AND lab_id IN ('lab-1-debian', 'lab-2-debian', 'lab-3-storage');
