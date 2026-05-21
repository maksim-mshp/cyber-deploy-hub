package lmsgateway

import "testing"

func TestMapperMapsMoodleIDsToLocalIDs(t *testing.T) {
	t.Parallel()

	mapper, err := NewMapper(`{"42":"course-linux"}`, `{"17":"LAB-02"}`)
	if err != nil {
		t.Fatalf("NewMapper: %v", err)
	}
	mapping, err := mapper.Map(LaunchRequest{
		MoodleUserID:       "user-1",
		MoodleCourseID:     "42",
		MoodleAssignmentID: "17",
	})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if mapping.StudentID != "moodle:user-1" {
		t.Fatalf("student_id = %s", mapping.StudentID)
	}
	if mapping.CourseID != "course-linux" {
		t.Fatalf("course_id = %s", mapping.CourseID)
	}
	if mapping.LabID != "LAB-02" {
		t.Fatalf("lab_id = %s", mapping.LabID)
	}
}

func TestMapperPrefersExplicitCourseAndLabIDs(t *testing.T) {
	t.Parallel()

	mapper, err := NewMapper(`{"42":"course-from-map"}`, `{"17":"lab-from-map"}`)
	if err != nil {
		t.Fatalf("NewMapper: %v", err)
	}
	mapping, err := mapper.Map(LaunchRequest{
		MoodleUserID:       "user-1",
		MoodleCourseID:     "42",
		MoodleAssignmentID: "17",
		CourseID:           "course-3",
		LabID:              "lab-1-debian",
	})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if mapping.CourseID != "course-3" {
		t.Fatalf("course_id = %s", mapping.CourseID)
	}
	if mapping.LabID != "lab-1-debian" {
		t.Fatalf("lab_id = %s", mapping.LabID)
	}
}
