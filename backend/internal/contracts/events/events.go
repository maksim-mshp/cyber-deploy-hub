package events

import "cyber-deploy-hub/internal/contracts"

const (
	LabRequestedV1          contracts.Subject = "evt.lab.requested.v1"
	LabReadyV1              contracts.Subject = "evt.lab.ready.v1"
	LabFailedV1             contracts.Subject = "evt.lab.failed.v1"
	LabFinishedV1           contracts.Subject = "evt.lab.finished.v1"
	LabVerifiedV1           contracts.Subject = "evt.lab.verified.v1"
	LabVerificationFailedV1 contracts.Subject = "evt.lab.verification_failed.v1"

	ProjectAllocatedV1        contracts.Subject = "evt.project.allocated.v1"
	ProjectAllocationFailedV1 contracts.Subject = "evt.project.allocation_failed.v1"
	ProjectReleasedV1         contracts.Subject = "evt.project.released.v1"

	CapacityApprovedV1            contracts.Subject = "evt.capacity.approved.v1"
	CapacityDeniedV1              contracts.Subject = "evt.capacity.denied.v1"
	CapacityReservationReleasedV1 contracts.Subject = "evt.capacity.reservation_released.v1"

	CloudVDIDeployedV1   contracts.Subject = "evt.cloud.vdi_deployed.v1"
	CloudDeployFailedV1  contracts.Subject = "evt.cloud.deploy_failed.v1"
	CloudLabCleanedV1    contracts.Subject = "evt.cloud.lab_cleaned.v1"
	CloudCleanupFailedV1 contracts.Subject = "evt.cloud.cleanup_failed.v1"

	VDIAccessIssuedV1  contracts.Subject = "evt.vdi.access_issued.v1"
	VDIAccessRevokedV1 contracts.Subject = "evt.vdi.access_revoked.v1"
	VDIAccessFailedV1  contracts.Subject = "evt.vdi.access_failed.v1"

	LifecycleCleanupScheduledV1 contracts.Subject = "evt.lifecycle.cleanup_scheduled.v1"
	LifecycleCleanupDueV1       contracts.Subject = "evt.lifecycle.cleanup_due.v1"
	LifecycleLabFrozenV1        contracts.Subject = "evt.lifecycle.lab_frozen.v1"

	CheckerCompletedV1 contracts.Subject = "evt.checker.completed.v1"
	CheckerFailedV1    contracts.Subject = "evt.checker.failed.v1"

	SettingsChangedV1 contracts.Subject = "evt.settings.changed.v1"
	AuditWrittenV1    contracts.Subject = "evt.audit.written.v1"
)

type LabRunEventPayload struct {
	LabRunID string `json:"lab_run_id"`
	State    string `json:"state,omitempty"`
}

type FailurePayload struct {
	LabRunID string `json:"lab_run_id"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type ProjectAllocatedV1Payload struct {
	LabRunID  string `json:"lab_run_id"`
	ProjectID string `json:"project_id"`
	DomainID  string `json:"domain_id"`
}

type CapacityDecisionV1Payload struct {
	LabRunID         string  `json:"lab_run_id"`
	ProjectID        string  `json:"project_id"`
	Approved         bool    `json:"approved"`
	PredictedCPU     float64 `json:"predicted_cpu"`
	PredictedRAM     float64 `json:"predicted_ram"`
	PredictedStorage float64 `json:"predicted_storage"`
	Threshold        float64 `json:"threshold"`
	Reason           string  `json:"reason,omitempty"`
}

type CloudVDIDeployedV1Payload struct {
	LabRunID  string             `json:"lab_run_id"`
	ProjectID string             `json:"project_id"`
	Instances []DeployedInstance `json:"instances"`
}

type DeployedInstance struct {
	Name       string `json:"name"`
	ServerID   string `json:"server_id"`
	VolumeID   string `json:"volume_id,omitempty"`
	PortID     string `json:"port_id,omitempty"`
	InternalIP string `json:"internal_ip,omitempty"`
}

type VDIAccessIssuedV1Payload struct {
	LabRunID  string `json:"lab_run_id"`
	StudentID string `json:"student_id"`
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

type LifecycleCleanupScheduledV1Payload struct {
	LabRunID string `json:"lab_run_id"`
	DueAt    string `json:"due_at"`
}

type CheckerCompletedV1Payload struct {
	LabRunID string            `json:"lab_run_id"`
	Passed   bool              `json:"passed"`
	Results  []CheckStepResult `json:"results"`
}

type CheckStepResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

type SettingsChangedV1Payload struct {
	ChangedBy string         `json:"changed_by"`
	Values    map[string]any `json:"values"`
}
