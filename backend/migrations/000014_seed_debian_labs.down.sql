DELETE FROM core.lab_definitions
WHERE course_id = 'course-3'
  AND lab_id IN ('lab-1-debian', 'lab-2-debian');
