package commands

const RequestProvisionV1 = "cmd.lab.request_provision.v1"

type RequestProvisionV1Payload struct {
	LabRunID  string `json:"lab_run_id"`
	StudentID string `json:"student_id"`
	CourseID  string `json:"course_id"`
	LabID     string `json:"lab_id"`
	Source    string `json:"source"`
}
