package lmsgateway

import (
	"encoding/json"
	"time"
)

const (
	LaunchStatusAccepted        = "ACCEPTED"
	LaunchStatusAlreadyAccepted = "ALREADY_ACCEPTED"
	LaunchStatusActiveLabExists = "ACTIVE_LAB_EXISTS"
)

type LaunchRequest struct {
	MoodleUserID       string `json:"moodle_user_id"`
	MoodleCourseID     string `json:"moodle_course_id"`
	MoodleAssignmentID string `json:"moodle_assignment_id"`
	UserLogin          string `json:"user_login,omitempty"`
	CourseName         string `json:"course_name,omitempty"`
	CourseID           string `json:"course_id,omitempty"`
	LabID              string `json:"lab_id,omitempty"`
	IdempotencyKey     string `json:"idempotency_key,omitempty"`
}

type Mapping struct {
	StudentID string `json:"student_id"`
	CourseID  string `json:"course_id"`
	LabID     string `json:"lab_id"`
}

type LaunchAccepted struct {
	LaunchID  string    `json:"launch_id"`
	LabRunID  string    `json:"lab_run_id"`
	SagaID    string    `json:"saga_id"`
	CommandID string    `json:"command_id"`
	Status    string    `json:"status"`
	Mapping   Mapping   `json:"mapping"`
	CreatedAt time.Time `json:"created_at"`
}

type LaunchRecord struct {
	ID                   string
	IdempotencyKey       string
	ExternalUserID       string
	ExternalCourseID     string
	ExternalAssignmentID string
	ExternalUserLogin    string
	ExternalCourseName   string
	LocalStudentID       string
	LocalCourseID        string
	LocalLabID           string
	LabRunID             string
	SagaID               string
	CommandID            string
	Status               string
	RequestPayload       json.RawMessage
	CreatedAt            time.Time
}

func (r LaunchRecord) Accepted() LaunchAccepted {
	return LaunchAccepted{
		LaunchID:  r.ID,
		LabRunID:  r.LabRunID,
		SagaID:    r.SagaID,
		CommandID: r.CommandID,
		Status:    r.Status,
		Mapping: Mapping{
			StudentID: r.LocalStudentID,
			CourseID:  r.LocalCourseID,
			LabID:     r.LocalLabID,
		},
		CreatedAt: r.CreatedAt,
	}
}

type MappingDescription struct {
	StudentIDRule      string            `json:"student_id_rule"`
	CourseIDRule       string            `json:"course_id_rule"`
	LabIDRule          string            `json:"lab_id_rule"`
	ConfiguredCourses  map[string]string `json:"configured_courses,omitempty"`
	ConfiguredLabs     map[string]string `json:"configured_labs,omitempty"`
	LaunchEndpoint     string            `json:"launch_endpoint"`
	ResultEndpoint     string            `json:"result_endpoint"`
	SignatureAlgorithm string            `json:"signature_algorithm"`
}

type LaunchResult struct {
	LaunchID       string  `json:"launch_id"`
	LabRunID       string  `json:"lab_run_id"`
	Status         string  `json:"status"`
	LabState       string  `json:"lab_state,omitempty"`
	FailureCode    string  `json:"failure_code,omitempty"`
	FailureMessage string  `json:"failure_message,omitempty"`
	CheckState     string  `json:"check_state,omitempty"`
	CheckPassed    *bool   `json:"check_passed,omitempty"`
	CheckError     string  `json:"check_error,omitempty"`
	Mapping        Mapping `json:"mapping"`
}
