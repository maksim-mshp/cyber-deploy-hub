package core

import (
	"context"
	"encoding/json"
	"fmt"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/contracts/events"
	"cyber-deploy-hub/internal/domain"
)

const (
	aggregateTypeLabRun = "lab_run"
	sagaTypeProvision   = "LabProvisioningSaga"
	sagaTypeCleanup     = "LabCleanupSaga"
)

type Repository interface {
	StartProvisioning(ctx context.Context, req commands.RequestProvisionV1Payload, command contracts.Envelope, next contracts.Envelope) error
	Advance(ctx context.Context, transition Transition) error
	Fail(ctx context.Context, failure Failure) error
	LoadLabRun(ctx context.Context, labRunID string) (LabRun, error)
}

type Transition struct {
	LabRunID       string
	ProjectID      string
	VDIURL         string
	State          domain.LabRunState
	StepName       string
	Message        contracts.Envelope
	ExpectedStates []domain.LabRunState
	Next           []contracts.Envelope
}

type Failure struct {
	LabRunID       string
	Code           string
	Message        string
	StepName       string
	Event          contracts.Envelope
	ExpectedStates []domain.LabRunState
	Next           []contracts.Envelope
}

type LabRun struct {
	ID        string
	StudentID string
	CourseID  string
	LabID     string
	ProjectID string
	State     domain.LabRunState
	Resources commands.LabResourceProfile
	Instances []commands.VMBlueprint
}

type Service struct {
	producer string
	repo     Repository
}

func NewService(producer string, repo Repository) *Service {
	return &Service{producer: producer, repo: repo}
}

func (s *Service) Handle(ctx context.Context, envelope contracts.Envelope) error {
	switch envelope.MessageType {
	case commands.RequestProvisionV1.String():
		return s.handleRequestProvision(ctx, envelope)
	case commands.RequestFreezeV1.String():
		return s.handleRequestFreeze(ctx, envelope)
	case commands.RequestVerificationV1.String():
		return s.handleRequestVerification(ctx, envelope)
	case commands.RequestCleanupV1.String():
		return s.handleRequestCleanup(ctx, envelope)
	case events.ProjectAllocatedV1.String():
		return s.handleProjectAllocated(ctx, envelope)
	case events.ProjectAllocationFailedV1.String():
		return s.failFromEvent(ctx, envelope, "PROJECT_ALLOCATION_FAILED", "project allocation failed", []domain.LabRunState{domain.LabRunAllocatingProject})
	case events.CapacityApprovedV1.String():
		return s.handleCapacityApproved(ctx, envelope)
	case events.CapacityDeniedV1.String():
		return s.handleCapacityDenied(ctx, envelope)
	case events.CloudVDIDeployedV1.String():
		return s.handleCloudVDIDeployed(ctx, envelope)
	case events.CloudDeployFailedV1.String():
		return s.failWithCloudCleanup(ctx, envelope, "CLOUD_DEPLOY_FAILED", "cloud deployment failed", false, []domain.LabRunState{domain.LabRunDeploying})
	case events.VDIAccessIssuedV1.String():
		return s.handleVDIAccessIssued(ctx, envelope)
	case events.VDIAccessFailedV1.String():
		return s.failWithCloudCleanup(ctx, envelope, "VDI_ACCESS_FAILED", "vdi access failed", true, []domain.LabRunState{domain.LabRunIssuingVDIAccess})
	case events.LifecycleCleanupScheduledV1.String():
		return s.record(ctx, envelope, domain.LabRunReady, "cleanup_scheduled")
	case events.LifecycleCleanupDueV1.String():
		return s.handleCleanupDue(ctx, envelope)
	case events.LifecycleLabFrozenV1.String():
		return s.record(ctx, envelope, domain.LabRunFrozen, "lab_frozen")
	case events.CloudLabCleanedV1.String():
		return s.handleCloudLabCleaned(ctx, envelope)
	case events.CloudCleanupFailedV1.String():
		return s.failFromEvent(ctx, envelope, "CLOUD_CLEANUP_FAILED", "cloud cleanup failed", []domain.LabRunState{domain.LabRunCleaning, domain.LabRunFailed})
	case events.ProjectReleasedV1.String():
		return s.handleProjectReleased(ctx, envelope)
	case events.CheckerCompletedV1.String():
		return s.handleCheckerCompleted(ctx, envelope)
	case events.CheckerFailedV1.String():
		return s.failFromEvent(ctx, envelope, "CHECKER_FAILED", "checker failed", []domain.LabRunState{domain.LabRunVerifying})
	default:
		return nil
	}
}

func (s *Service) handleRequestProvision(ctx context.Context, envelope contracts.Envelope) error {
	var payload commands.RequestProvisionV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}

	next, err := s.newCommand(envelope, commands.ProjectAllocateV1, commands.ProjectAllocateV1Payload{
		LabRunID:  payload.LabRunID,
		StudentID: payload.StudentID,
		CourseID:  payload.CourseID,
		LabID:     payload.LabID,
	})
	if err != nil {
		return err
	}

	return s.repo.StartProvisioning(ctx, payload, envelope, next)
}

func (s *Service) handleProjectAllocated(ctx context.Context, envelope contracts.Envelope) error {
	var payload events.ProjectAllocatedV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	labRun, err := s.repo.LoadLabRun(ctx, payload.LabRunID)
	if err != nil {
		return err
	}

	next, err := s.newCommand(envelope, commands.CapacityCheckV1, commands.CapacityCheckV1Payload{
		LabRunID:  payload.LabRunID,
		ProjectID: payload.ProjectID,
		Resources: resourceProfileOrDefault(labRun.Resources),
	})
	if err != nil {
		return err
	}

	return s.repo.Advance(ctx, Transition{
		LabRunID:       payload.LabRunID,
		ProjectID:      payload.ProjectID,
		State:          domain.LabRunCheckingCapacity,
		StepName:       "project_allocated",
		Message:        envelope,
		ExpectedStates: []domain.LabRunState{domain.LabRunAllocatingProject},
		Next:           []contracts.Envelope{next},
	})
}

func (s *Service) handleRequestFreeze(ctx context.Context, envelope contracts.Envelope) error {
	var payload commands.LabRunCommandPayload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	if payload.LabRunID == "" {
		return fmt.Errorf("freeze command does not include lab_run_id")
	}
	next, err := s.newCommand(envelope, commands.LifecycleFreezeLabV1, commands.LifecycleFreezeLabV1Payload{
		LabRunID: payload.LabRunID,
		Reason:   payload.Reason,
	})
	if err != nil {
		return err
	}
	return s.repo.Advance(ctx, Transition{
		LabRunID:       payload.LabRunID,
		State:          domain.LabRunFrozen,
		StepName:       "request_freeze",
		Message:        envelope,
		ExpectedStates: activeLabStates(),
		Next:           []contracts.Envelope{next},
	})
}

func (s *Service) handleRequestCleanup(ctx context.Context, envelope contracts.Envelope) error {
	var payload commands.LabRunCommandPayload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	if payload.LabRunID == "" {
		return fmt.Errorf("cleanup command does not include lab_run_id")
	}
	labRun, err := s.repo.LoadLabRun(ctx, payload.LabRunID)
	if err != nil {
		return err
	}
	reason := payload.Reason
	if reason == "" {
		reason = "manual_cleanup"
	}
	revoke, err := s.newCommand(envelope, commands.VDIRevokeAccessV1, commands.VDIRevokeAccessV1Payload{
		LabRunID: payload.LabRunID,
		Reason:   reason,
	})
	if err != nil {
		return err
	}
	cleanup, err := s.newCommand(envelope, commands.CloudCleanupLabV1, commands.CloudCleanupLabV1Payload{
		LabRunID:  payload.LabRunID,
		ProjectID: labRun.ProjectID,
		Reason:    reason,
	})
	if err != nil {
		return err
	}
	cancelTimer, err := s.newCommand(envelope, commands.LifecycleCancelTimerV1, commands.LifecycleCancelTimerV1Payload{
		LabRunID: payload.LabRunID,
		Reason:   reason,
	})
	if err != nil {
		return err
	}
	return s.repo.Advance(ctx, Transition{
		LabRunID:       payload.LabRunID,
		State:          domain.LabRunCleaning,
		StepName:       "request_cleanup",
		Message:        envelope,
		ExpectedStates: cleanupAllowedStates(),
		Next:           []contracts.Envelope{revoke, cleanup, cancelTimer},
	})
}

func (s *Service) handleRequestVerification(ctx context.Context, envelope contracts.Envelope) error {
	var payload commands.LabRunCommandPayload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	if payload.LabRunID == "" {
		return fmt.Errorf("verification command does not include lab_run_id")
	}
	profileID := payload.Reason
	if profileID == "" {
		profileID = "default"
	}
	next, err := s.newCommand(envelope, commands.CheckerRunV1, commands.CheckerRunV1Payload{
		LabRunID:  payload.LabRunID,
		ProfileID: profileID,
	})
	if err != nil {
		return err
	}
	return s.repo.Advance(ctx, Transition{
		LabRunID:       payload.LabRunID,
		State:          domain.LabRunVerifying,
		StepName:       "request_verification",
		Message:        envelope,
		ExpectedStates: []domain.LabRunState{domain.LabRunReady, domain.LabRunVerified, domain.LabRunVerificationFailed},
		Next:           []contracts.Envelope{next},
	})
}

func (s *Service) handleCapacityApproved(ctx context.Context, envelope contracts.Envelope) error {
	var payload events.CapacityDecisionV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	labRun, err := s.repo.LoadLabRun(ctx, payload.LabRunID)
	if err != nil {
		return err
	}

	next, err := s.newCommand(envelope, commands.CloudDeployVDIV1, commands.CloudDeployVDIV1Payload{
		LabRunID:  payload.LabRunID,
		ProjectID: payload.ProjectID,
		Instances: append([]commands.VMBlueprint(nil), labRun.Instances...),
	})
	if err != nil {
		return err
	}

	return s.repo.Advance(ctx, Transition{
		LabRunID:       payload.LabRunID,
		ProjectID:      payload.ProjectID,
		State:          domain.LabRunDeploying,
		StepName:       "capacity_approved",
		Message:        envelope,
		ExpectedStates: []domain.LabRunState{domain.LabRunCheckingCapacity},
		Next:           []contracts.Envelope{next},
	})
}

func (s *Service) handleCapacityDenied(ctx context.Context, envelope contracts.Envelope) error {
	var payload events.CapacityDecisionV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}

	next, err := s.newCommand(envelope, commands.ProjectReleaseV1, commands.ProjectReleaseV1Payload{
		LabRunID:  payload.LabRunID,
		ProjectID: payload.ProjectID,
		Reason:    "capacity_denied",
	})
	if err != nil {
		return err
	}
	failed, err := s.failureEvent(envelope, payload.LabRunID, "CAPACITY_DENIED", payload.Reason)
	if err != nil {
		return err
	}

	return s.repo.Fail(ctx, Failure{
		LabRunID:       payload.LabRunID,
		Code:           "CAPACITY_DENIED",
		Message:        payload.Reason,
		StepName:       "capacity_denied",
		Event:          envelope,
		ExpectedStates: []domain.LabRunState{domain.LabRunCheckingCapacity},
		Next:           []contracts.Envelope{next, failed},
	})
}

func resourceProfileOrDefault(profile commands.LabResourceProfile) commands.LabResourceProfile {
	if profile.VCPU > 0 && profile.RAMMiB > 0 && profile.DiskGiB > 0 {
		return profile
	}
	return commands.LabResourceProfile{
		VCPU:    9,
		RAMMiB:  16 * 1024,
		DiskGiB: 214,
	}
}

func (s *Service) handleCloudVDIDeployed(ctx context.Context, envelope contracts.Envelope) error {
	var payload events.CloudVDIDeployedV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}

	labRun, err := s.repo.LoadLabRun(ctx, payload.LabRunID)
	if err != nil {
		return err
	}
	next, err := s.newCommand(envelope, commands.VDIIssueAccessV1, commands.VDIIssueAccessV1Payload{
		LabRunID:  payload.LabRunID,
		StudentID: labRun.StudentID,
		ProjectID: payload.ProjectID,
	})
	if err != nil {
		return err
	}

	return s.repo.Advance(ctx, Transition{
		LabRunID:       payload.LabRunID,
		ProjectID:      payload.ProjectID,
		State:          domain.LabRunIssuingVDIAccess,
		StepName:       "cloud_vdi_deployed",
		Message:        envelope,
		ExpectedStates: []domain.LabRunState{domain.LabRunDeploying},
		Next:           []contracts.Envelope{next},
	})
}

func (s *Service) handleVDIAccessIssued(ctx context.Context, envelope contracts.Envelope) error {
	var payload events.VDIAccessIssuedV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}

	schedule, err := s.newCommand(envelope, commands.LifecycleScheduleCleanupV1, commands.LifecycleScheduleCleanupV1Payload{
		LabRunID:      payload.LabRunID,
		TTLSeconds:    2 * 60 * 60,
		FreezeSeconds: 24 * 60 * 60,
	})
	if err != nil {
		return err
	}
	ready, err := s.newEvent(envelope, events.LabReadyV1, events.LabRunEventPayload{
		LabRunID: payload.LabRunID,
		State:    string(domain.LabRunReady),
	})
	if err != nil {
		return err
	}

	return s.repo.Advance(ctx, Transition{
		LabRunID:       payload.LabRunID,
		VDIURL:         payload.URL,
		State:          domain.LabRunReady,
		StepName:       "vdi_access_issued",
		Message:        envelope,
		ExpectedStates: []domain.LabRunState{domain.LabRunIssuingVDIAccess},
		Next:           []contracts.Envelope{schedule, ready},
	})
}

func (s *Service) handleCleanupDue(ctx context.Context, envelope contracts.Envelope) error {
	var payload events.LabRunEventPayload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}

	labRun, err := s.repo.LoadLabRun(ctx, payload.LabRunID)
	if err != nil {
		return err
	}
	revoke, err := s.newCommand(envelope, commands.VDIRevokeAccessV1, commands.VDIRevokeAccessV1Payload{
		LabRunID: payload.LabRunID,
		Reason:   "cleanup_due",
	})
	if err != nil {
		return err
	}
	cleanup, err := s.newCommand(envelope, commands.CloudCleanupLabV1, commands.CloudCleanupLabV1Payload{
		LabRunID:  payload.LabRunID,
		ProjectID: labRun.ProjectID,
		Reason:    "cleanup_due",
	})
	if err != nil {
		return err
	}

	return s.repo.Advance(ctx, Transition{
		LabRunID:       payload.LabRunID,
		State:          domain.LabRunCleaning,
		StepName:       "cleanup_due",
		Message:        envelope,
		ExpectedStates: cleanupAllowedStates(),
		Next:           []contracts.Envelope{revoke, cleanup},
	})
}

func (s *Service) handleCloudLabCleaned(ctx context.Context, envelope contracts.Envelope) error {
	var payload events.LabRunEventPayload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	labRun, err := s.repo.LoadLabRun(ctx, payload.LabRunID)
	if err != nil {
		return err
	}
	state := domain.LabRunCleaning
	if labRun.State == domain.LabRunFailed {
		state = domain.LabRunFailed
	}
	next, err := s.newCommand(envelope, commands.ProjectReleaseV1, commands.ProjectReleaseV1Payload{
		LabRunID:  payload.LabRunID,
		ProjectID: labRun.ProjectID,
		Reason:    "cloud_cleaned",
	})
	if err != nil {
		return err
	}
	return s.repo.Advance(ctx, Transition{
		LabRunID:       payload.LabRunID,
		State:          state,
		StepName:       "cloud_lab_cleaned",
		Message:        envelope,
		ExpectedStates: []domain.LabRunState{domain.LabRunCleaning, domain.LabRunFailed},
		Next:           []contracts.Envelope{next},
	})
}

func (s *Service) handleProjectReleased(ctx context.Context, envelope contracts.Envelope) error {
	labRunID, err := labRunIDFromPayload(envelope)
	if err != nil {
		return err
	}
	labRun, err := s.repo.LoadLabRun(ctx, labRunID)
	if err != nil {
		return err
	}
	if labRun.State == domain.LabRunFailed {
		return s.repo.Advance(ctx, Transition{
			LabRunID:       labRunID,
			State:          domain.LabRunFailed,
			StepName:       "project_released",
			Message:        envelope,
			ExpectedStates: []domain.LabRunState{domain.LabRunFailed},
		})
	}

	finished, err := s.newEvent(envelope, events.LabFinishedV1, events.LabRunEventPayload{
		LabRunID: labRunID,
		State:    string(domain.LabRunFinished),
	})
	if err != nil {
		return err
	}
	return s.repo.Advance(ctx, Transition{
		LabRunID:       labRunID,
		State:          domain.LabRunFinished,
		StepName:       "project_released",
		Message:        envelope,
		ExpectedStates: []domain.LabRunState{domain.LabRunCleaning},
		Next:           []contracts.Envelope{finished},
	})
}

func (s *Service) handleCheckerCompleted(ctx context.Context, envelope contracts.Envelope) error {
	var payload events.CheckerCompletedV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	state := domain.LabRunVerificationFailed
	subject := events.LabVerificationFailedV1
	if payload.Passed {
		state = domain.LabRunVerified
		subject = events.LabVerifiedV1
	}
	next, err := s.newEvent(envelope, subject, events.LabRunEventPayload{
		LabRunID: payload.LabRunID,
		State:    string(state),
	})
	if err != nil {
		return err
	}
	return s.repo.Advance(ctx, Transition{
		LabRunID:       payload.LabRunID,
		State:          state,
		StepName:       "checker_completed",
		Message:        envelope,
		ExpectedStates: []domain.LabRunState{domain.LabRunVerifying},
		Next:           []contracts.Envelope{next},
	})
}

func (s *Service) failFromEvent(ctx context.Context, envelope contracts.Envelope, code string, fallback string, expectedStates []domain.LabRunState) error {
	labRunID, message := failureFromEnvelope(envelope, fallback)
	if labRunID == "" {
		return fmt.Errorf("failed event %s does not include lab_run_id", envelope.MessageType)
	}
	failed, err := s.failureEvent(envelope, labRunID, code, message)
	if err != nil {
		return err
	}
	return s.repo.Fail(ctx, Failure{
		LabRunID:       labRunID,
		Code:           code,
		Message:        message,
		StepName:       envelope.MessageType,
		Event:          envelope,
		ExpectedStates: expectedStates,
		Next:           []contracts.Envelope{failed},
	})
}

func (s *Service) failWithCloudCleanup(ctx context.Context, envelope contracts.Envelope, code string, fallback string, revokeVDI bool, expectedStates []domain.LabRunState) error {
	labRunID, message := failureFromEnvelope(envelope, fallback)
	if labRunID == "" {
		return fmt.Errorf("failed event %s does not include lab_run_id", envelope.MessageType)
	}

	next := make([]contracts.Envelope, 0, 3)
	failed, err := s.failureEvent(envelope, labRunID, code, message)
	if err != nil {
		return err
	}
	next = append(next, failed)

	labRun, err := s.repo.LoadLabRun(ctx, labRunID)
	if err != nil {
		return err
	}
	if revokeVDI {
		revoke, err := s.newCommand(envelope, commands.VDIRevokeAccessV1, commands.VDIRevokeAccessV1Payload{
			LabRunID: labRunID,
			Reason:   code,
		})
		if err != nil {
			return err
		}
		next = append(next, revoke)
	}
	if labRun.ProjectID != "" {
		cleanup, err := s.newCommand(envelope, commands.CloudCleanupLabV1, commands.CloudCleanupLabV1Payload{
			LabRunID:  labRunID,
			ProjectID: labRun.ProjectID,
			Reason:    code,
		})
		if err != nil {
			return err
		}
		next = append(next, cleanup)
	}

	return s.repo.Fail(ctx, Failure{
		LabRunID:       labRunID,
		Code:           code,
		Message:        message,
		StepName:       envelope.MessageType,
		Event:          envelope,
		ExpectedStates: expectedStates,
		Next:           next,
	})
}

func (s *Service) record(ctx context.Context, envelope contracts.Envelope, state domain.LabRunState, stepName string) error {
	labRunID, err := labRunIDFromPayload(envelope)
	if err != nil {
		return err
	}
	return s.repo.Advance(ctx, Transition{
		LabRunID:       labRunID,
		State:          state,
		StepName:       stepName,
		Message:        envelope,
		ExpectedStates: []domain.LabRunState{state},
	})
}

func activeLabStates() []domain.LabRunState {
	return []domain.LabRunState{
		domain.LabRunReady,
		domain.LabRunVerifying,
		domain.LabRunVerified,
		domain.LabRunVerificationFailed,
		domain.LabRunFrozen,
	}
}

func cleanupAllowedStates() []domain.LabRunState {
	return []domain.LabRunState{
		domain.LabRunRequested,
		domain.LabRunAllocatingProject,
		domain.LabRunCheckingCapacity,
		domain.LabRunDeploying,
		domain.LabRunIssuingVDIAccess,
		domain.LabRunReady,
		domain.LabRunVerifying,
		domain.LabRunVerified,
		domain.LabRunVerificationFailed,
		domain.LabRunFrozen,
	}
}

func (s *Service) newCommand(cause contracts.Envelope, subject contracts.Subject, payload any) (contracts.Envelope, error) {
	return contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:           contracts.MessageKindCommand,
		Type:           subject,
		Producer:       s.producer,
		CorrelationID:  cause.CorrelationID,
		CausationID:    cause.MessageID,
		SagaID:         cause.SagaID,
		AggregateType:  aggregateTypeLabRun,
		AggregateID:    cause.AggregateID,
		IdempotencyKey: idempotencyKey(cause, subject),
		Payload:        payload,
	})
}

func (s *Service) newEvent(cause contracts.Envelope, subject contracts.Subject, payload any) (contracts.Envelope, error) {
	return contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:           contracts.MessageKindEvent,
		Type:           subject,
		Producer:       s.producer,
		CorrelationID:  cause.CorrelationID,
		CausationID:    cause.MessageID,
		SagaID:         cause.SagaID,
		AggregateType:  aggregateTypeLabRun,
		AggregateID:    cause.AggregateID,
		IdempotencyKey: idempotencyKey(cause, subject),
		Payload:        payload,
	})
}

func idempotencyKey(cause contracts.Envelope, subject contracts.Subject) string {
	if cause.SagaID != "" {
		return cause.SagaID + ":" + subject.String()
	}
	return cause.AggregateID + ":" + cause.MessageID + ":" + subject.String()
}

func (s *Service) failureEvent(cause contracts.Envelope, labRunID string, code string, message string) (contracts.Envelope, error) {
	return s.newEvent(cause, events.LabFailedV1, events.FailurePayload{
		LabRunID: labRunID,
		Code:     code,
		Message:  message,
	})
}

func decodePayload(envelope contracts.Envelope, dst any) error {
	if err := json.Unmarshal(envelope.Payload, dst); err != nil {
		return fmt.Errorf("decode %s payload: %w", envelope.MessageType, err)
	}
	return nil
}

func failureFromEnvelope(envelope contracts.Envelope, fallback string) (string, string) {
	labRunID, _ := labRunIDFromPayload(envelope)
	message := fallback
	if envelope.Error != nil && envelope.Error.Message != "" {
		message = envelope.Error.Message
	}
	var failure events.FailurePayload
	if err := json.Unmarshal(envelope.Payload, &failure); err == nil {
		if failure.LabRunID != "" {
			labRunID = failure.LabRunID
		}
		if failure.Message != "" {
			message = failure.Message
		}
	}
	return labRunID, message
}

func labRunIDFromPayload(envelope contracts.Envelope) (string, error) {
	var payload struct {
		LabRunID string `json:"lab_run_id"`
	}
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return "", fmt.Errorf("decode %s lab_run_id: %w", envelope.MessageType, err)
	}
	return payload.LabRunID, nil
}
