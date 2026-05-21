package commands

import "cyber-deploy-hub/internal/contracts"

const (
	RequestProvisionV1    contracts.Subject = "cmd.lab.request_provision.v1"
	RequestFreezeV1       contracts.Subject = "cmd.lab.request_freeze.v1"
	RequestVerificationV1 contracts.Subject = "cmd.lab.request_verification.v1"
	RequestCleanupV1      contracts.Subject = "cmd.lab.request_cleanup.v1"
	RequestVDIAccessV1    contracts.Subject = "cmd.lab.request_vdi_access.v1"

	ProjectAllocateV1 contracts.Subject = "cmd.project.allocate.v1"
	ProjectReleaseV1  contracts.Subject = "cmd.project.release.v1"

	CapacityCheckV1              contracts.Subject = "cmd.capacity.check.v1"
	CapacityReleaseReservationV1 contracts.Subject = "cmd.capacity.release_reservation.v1"

	CloudDeployVDIV1  contracts.Subject = "cmd.cloud.deploy_vdi.v1"
	CloudCleanupLabV1 contracts.Subject = "cmd.cloud.cleanup_lab.v1"

	VDIIssueAccessV1  contracts.Subject = "cmd.vdi.issue_access.v1"
	VDIRevokeAccessV1 contracts.Subject = "cmd.vdi.revoke_access.v1"

	LifecycleScheduleCleanupV1 contracts.Subject = "cmd.lifecycle.schedule_cleanup.v1"
	LifecycleFreezeLabV1       contracts.Subject = "cmd.lifecycle.freeze_lab.v1"
	LifecycleCancelTimerV1     contracts.Subject = "cmd.lifecycle.cancel_timer.v1"

	CheckerRunV1 contracts.Subject = "cmd.checker.run.v1"

	SettingsUpdateV1 contracts.Subject = "cmd.settings.update.v1"
	AuditWriteV1     contracts.Subject = "cmd.audit.write.v1"
)

type RequestProvisionV1Payload struct {
	LabRunID  string             `json:"lab_run_id"`
	StudentID string             `json:"student_id"`
	CourseID  string             `json:"course_id"`
	LabID     string             `json:"lab_id"`
	Source    string             `json:"source"`
	Resources LabResourceProfile `json:"resources,omitempty"`
	Instances []VMBlueprint      `json:"instances,omitempty"`
}

type LabRunCommandPayload struct {
	LabRunID string `json:"lab_run_id"`
	Reason   string `json:"reason,omitempty"`
}

type ProjectAllocateV1Payload struct {
	LabRunID        string `json:"lab_run_id"`
	StudentID       string `json:"student_id"`
	CourseID        string `json:"course_id"`
	LabID           string `json:"lab_id"`
	RequestedDomain string `json:"requested_domain,omitempty"`
}

type ProjectReleaseV1Payload struct {
	LabRunID  string `json:"lab_run_id"`
	ProjectID string `json:"project_id"`
	Reason    string `json:"reason,omitempty"`
}

type CapacityCheckV1Payload struct {
	LabRunID  string             `json:"lab_run_id"`
	ProjectID string             `json:"project_id"`
	Resources LabResourceProfile `json:"resources"`
}

type LabResourceProfile struct {
	VCPU    int   `json:"vcpu"`
	RAMMiB  int   `json:"ram_mib"`
	DiskGiB int64 `json:"disk_gib"`
}

type CloudDeployVDIV1Payload struct {
	LabRunID  string        `json:"lab_run_id"`
	ProjectID string        `json:"project_id"`
	Instances []VMBlueprint `json:"instances"`
}

type VMBlueprint struct {
	Name     string `json:"name"`
	ImageID  string `json:"image_id"`
	FlavorID string `json:"flavor_id"`
	FixedIP  string `json:"fixed_ip,omitempty"`
	DiskGiB  int64  `json:"disk_gib"`
}

type CloudCleanupLabV1Payload struct {
	LabRunID  string `json:"lab_run_id"`
	ProjectID string `json:"project_id"`
	Reason    string `json:"reason,omitempty"`
}

type VDIIssueAccessV1Payload struct {
	LabRunID  string `json:"lab_run_id"`
	StudentID string `json:"student_id"`
	ProjectID string `json:"project_id"`
}

type VDIRevokeAccessV1Payload struct {
	LabRunID string `json:"lab_run_id"`
	Reason   string `json:"reason,omitempty"`
}

type LifecycleScheduleCleanupV1Payload struct {
	LabRunID      string `json:"lab_run_id"`
	TTLSeconds    int64  `json:"ttl_seconds"`
	FreezeSeconds int64  `json:"freeze_seconds"`
}

type LifecycleFreezeLabV1Payload struct {
	LabRunID      string `json:"lab_run_id"`
	FreezeSeconds int64  `json:"freeze_seconds,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type LifecycleCancelTimerV1Payload struct {
	LabRunID string `json:"lab_run_id"`
	Reason   string `json:"reason,omitempty"`
}

type CheckerRunV1Payload struct {
	LabRunID  string `json:"lab_run_id"`
	ProfileID string `json:"profile_id"`
}

type SettingsUpdateV1Payload struct {
	ChangedBy string         `json:"changed_by"`
	Values    map[string]any `json:"values"`
}

type AuditWriteV1Payload struct {
	ActorID   string         `json:"actor_id"`
	Action    string         `json:"action"`
	Resource  string         `json:"resource"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt string         `json:"created_at,omitempty"`
}
