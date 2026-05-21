package readmodel

import (
	"encoding/json"
	"time"
)

type LabRunView struct {
	ID             string        `json:"id"`
	StudentID      string        `json:"student_id"`
	CourseID       string        `json:"course_id"`
	LabID          string        `json:"lab_id"`
	State          string        `json:"state"`
	FailureCode    string        `json:"failure_code,omitempty"`
	FailureMessage string        `json:"failure_message,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	Events         []LabRunEvent `json:"events"`
}

type LabRunEvent struct {
	ID          int64           `json:"id"`
	LabRunID    string          `json:"lab_run_id"`
	State       string          `json:"state"`
	MessageType string          `json:"message_type"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

type VDIAccessView struct {
	LabRunID  string `json:"lab_run_id"`
	Available bool   `json:"available"`
	URL       string `json:"url,omitempty"`
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
}

type SettingsView struct {
	Values map[string]any `json:"values"`
}

type AuditView struct {
	Events []LabRunEvent `json:"events"`
}

type ProjectPoolView struct {
	States   map[string]int    `json:"states"`
	Projects []ProjectPoolItem `json:"projects"`
}

type ProjectPoolItem struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	DomainID            string `json:"domain_id"`
	State               string `json:"state"`
	CurrentLabRunID     string `json:"current_lab_run_id,omitempty"`
	ReservedByStudentID string `json:"reserved_by_student_id,omitempty"`
}

type CheckRunsView struct {
	Runs []CheckRunView `json:"runs"`
}

type CheckRunView struct {
	ID           string                `json:"id"`
	LabRunID     string                `json:"lab_run_id"`
	ProfileID    string                `json:"profile_id"`
	State        string                `json:"state"`
	Passed       bool                  `json:"passed"`
	ErrorCode    string                `json:"error_code,omitempty"`
	ErrorMessage string                `json:"error_message,omitempty"`
	StartedAt    time.Time             `json:"started_at"`
	FinishedAt   time.Time             `json:"finished_at"`
	Results      []CheckStepResultView `json:"results"`
}

type CheckStepResultView struct {
	Sequence   int       `json:"sequence"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	Passed     bool      `json:"passed"`
	ExitCode   int       `json:"exit_code"`
	Message    string    `json:"message,omitempty"`
	StdoutTail string    `json:"stdout_tail,omitempty"`
	StderrTail string    `json:"stderr_tail,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}
