package domain

type LabRunState string

const (
	LabRunRequested          LabRunState = "REQUESTED"
	LabRunAllocatingProject  LabRunState = "ALLOCATING_PROJECT"
	LabRunCheckingCapacity   LabRunState = "CHECKING_CAPACITY"
	LabRunDeploying          LabRunState = "DEPLOYING"
	LabRunIssuingVDIAccess   LabRunState = "ISSUING_VDI_ACCESS"
	LabRunReady              LabRunState = "READY"
	LabRunVerifying          LabRunState = "VERIFYING"
	LabRunVerified           LabRunState = "VERIFIED"
	LabRunVerificationFailed LabRunState = "VERIFICATION_FAILED"
	LabRunFrozen             LabRunState = "FROZEN"
	LabRunCleaning           LabRunState = "CLEANING"
	LabRunFinished           LabRunState = "FINISHED"
	LabRunFailed             LabRunState = "FAILED"
)
