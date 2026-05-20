package domain

type LabRunState string

const (
	LabRunRequested LabRunState = "REQUESTED"
	LabRunFailed    LabRunState = "FAILED"
)
