package capacity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/contracts/events"
)

type Repository interface {
	SaveCheck(ctx context.Context, command contracts.Envelope, req commands.CapacityCheckV1Payload, snapshot Snapshot, decision Decision, next []contracts.Envelope) error
	ReleaseReservation(ctx context.Context, command contracts.Envelope, req commands.LabRunCommandPayload, next contracts.Envelope) error
}

type Service struct {
	producer  string
	provider  StatProvider
	quota     QuotaChecker
	repo      Repository
	threshold float64
}

func NewService(producer string, provider StatProvider, quota QuotaChecker, repo Repository, threshold float64) (*Service, error) {
	if producer == "" {
		return nil, errors.New("producer is empty")
	}
	if provider == nil {
		return nil, errors.New("stat provider is nil")
	}
	if quota == nil {
		quota = NoopQuotaChecker{}
	}
	if repo == nil {
		return nil, errors.New("repository is nil")
	}
	if threshold <= 0 || threshold > 100 {
		return nil, fmt.Errorf("threshold %.2f must be in (0,100]", threshold)
	}
	return &Service{
		producer:  producer,
		provider:  provider,
		quota:     quota,
		repo:      repo,
		threshold: threshold,
	}, nil
}

func (s *Service) Handle(ctx context.Context, envelope contracts.Envelope) error {
	switch envelope.MessageType {
	case commands.CapacityCheckV1.String():
		return s.handleCheck(ctx, envelope)
	case commands.CapacityReleaseReservationV1.String():
		return s.handleReleaseReservation(ctx, envelope)
	default:
		return nil
	}
}

func (s *Service) handleCheck(ctx context.Context, envelope contracts.Envelope) error {
	var payload commands.CapacityCheckV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	if err := validateCheck(payload); err != nil {
		return err
	}

	snapshot, err := s.provider.Current(ctx)
	if err != nil {
		snapshot = Snapshot{
			Source:          "unavailable",
			VCPUsTotal:      1,
			VCPUsFree:       0,
			RAMMiBTotal:     1,
			RAMMiBFree:      0,
			StorageGiBTotal: 1,
			StorageGiBUsed:  1,
			RawPayload:      json.RawMessage(`{}`),
		}
		decision := Decision{
			Approved:         false,
			PredictedCPU:     100,
			PredictedRAM:     100,
			PredictedStorage: 100,
			Threshold:        s.threshold,
			Reason:           "capacity stat unavailable: " + err.Error(),
		}
		next, err := s.deniedMessages(envelope, payload, decision)
		if err != nil {
			return err
		}
		return s.repo.SaveCheck(ctx, envelope, payload, snapshot, decision, next)
	}

	decision := Evaluate(snapshot, payload.Resources, s.threshold)
	quotaCheck, err := s.quota.Check(ctx, payload.ProjectID, payload.Resources)
	if err != nil {
		decision.Approved = false
		decision.Reason = joinReasons(decision.Reason, "project quota unavailable: "+err.Error())
	} else if !quotaCheck.Allowed {
		decision.Approved = false
		decision.Reason = joinReasons(decision.Reason, quotaCheck.Reason)
	}
	next, err := s.decisionMessages(envelope, payload, decision)
	if err != nil {
		return err
	}
	return s.repo.SaveCheck(ctx, envelope, payload, snapshot, decision, next)
}

func (s *Service) handleReleaseReservation(ctx context.Context, envelope contracts.Envelope) error {
	var payload commands.LabRunCommandPayload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	if payload.LabRunID == "" {
		return errors.New("lab_run_id is required")
	}
	event, err := s.newEvent(envelope, events.CapacityReservationReleasedV1, events.LabRunEventPayload{
		LabRunID: payload.LabRunID,
		State:    reservationStateReleased,
	})
	if err != nil {
		return err
	}
	return s.repo.ReleaseReservation(ctx, envelope, payload, event)
}

func (s *Service) decisionMessages(envelope contracts.Envelope, payload commands.CapacityCheckV1Payload, decision Decision) ([]contracts.Envelope, error) {
	if decision.Approved {
		event, err := s.newEvent(envelope, events.CapacityApprovedV1, events.CapacityDecisionV1Payload{
			LabRunID:         payload.LabRunID,
			ProjectID:        payload.ProjectID,
			Approved:         true,
			PredictedCPU:     decision.PredictedCPU,
			PredictedRAM:     decision.PredictedRAM,
			PredictedStorage: decision.PredictedStorage,
			Threshold:        decision.Threshold,
		})
		if err != nil {
			return nil, err
		}
		return []contracts.Envelope{event}, nil
	}
	return s.deniedMessages(envelope, payload, decision)
}

func (s *Service) deniedMessages(envelope contracts.Envelope, payload commands.CapacityCheckV1Payload, decision Decision) ([]contracts.Envelope, error) {
	denied, err := s.newEvent(envelope, events.CapacityDeniedV1, events.CapacityDecisionV1Payload{
		LabRunID:         payload.LabRunID,
		ProjectID:        payload.ProjectID,
		Approved:         false,
		PredictedCPU:     decision.PredictedCPU,
		PredictedRAM:     decision.PredictedRAM,
		PredictedStorage: decision.PredictedStorage,
		Threshold:        decision.Threshold,
		Reason:           decision.Reason,
	})
	if err != nil {
		return nil, err
	}
	denied.Error = &contracts.MessageError{
		Code:    "CAPACITY_DENIED",
		Message: decision.Reason,
	}

	audit, err := s.newCommand(envelope, commands.AuditWriteV1, commands.AuditWriteV1Payload{
		ActorID:  s.producer,
		Action:   "capacity.denied",
		Resource: payload.LabRunID,
		Metadata: map[string]any{
			"project_id":        payload.ProjectID,
			"predicted_cpu":     decision.PredictedCPU,
			"predicted_ram":     decision.PredictedRAM,
			"predicted_storage": decision.PredictedStorage,
			"threshold":         decision.Threshold,
			"reason":            decision.Reason,
		},
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, err
	}
	return []contracts.Envelope{denied, audit}, nil
}

func (s *Service) newEvent(cause contracts.Envelope, subject contracts.Subject, payload any) (contracts.Envelope, error) {
	envelope, err := contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:           contracts.MessageKindEvent,
		Type:           subject,
		Producer:       s.producer,
		CorrelationID:  cause.CorrelationID,
		CausationID:    cause.MessageID,
		SagaID:         cause.SagaID,
		AggregateType:  "lab_run",
		AggregateID:    cause.AggregateID,
		IdempotencyKey: cause.SagaID + ":" + subject.String(),
		Payload:        payload,
	})
	if err != nil {
		return contracts.Envelope{}, err
	}
	envelope.MessageID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(cause.MessageID+":"+subject.String())).String()
	return envelope, nil
}

func (s *Service) newCommand(cause contracts.Envelope, subject contracts.Subject, payload any) (contracts.Envelope, error) {
	envelope, err := contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:           contracts.MessageKindCommand,
		Type:           subject,
		Producer:       s.producer,
		CorrelationID:  cause.CorrelationID,
		CausationID:    cause.MessageID,
		SagaID:         cause.SagaID,
		AggregateType:  "lab_run",
		AggregateID:    cause.AggregateID,
		IdempotencyKey: cause.SagaID + ":" + subject.String(),
		Payload:        payload,
	})
	if err != nil {
		return contracts.Envelope{}, err
	}
	envelope.MessageID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(cause.MessageID+":"+subject.String())).String()
	return envelope, nil
}

func validateCheck(payload commands.CapacityCheckV1Payload) error {
	if payload.LabRunID == "" {
		return errors.New("lab_run_id is required")
	}
	if payload.ProjectID == "" {
		return errors.New("project_id is required")
	}
	if payload.Resources.VCPU <= 0 {
		return errors.New("resources.vcpu must be positive")
	}
	if payload.Resources.RAMMiB <= 0 {
		return errors.New("resources.ram_mib must be positive")
	}
	if payload.Resources.DiskGiB <= 0 {
		return errors.New("resources.disk_gib must be positive")
	}
	return nil
}

func joinReasons(values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "; ")
}

func decodePayload(envelope contracts.Envelope, dst any) error {
	if err := json.Unmarshal(envelope.Payload, dst); err != nil {
		return fmt.Errorf("decode %s payload: %w", envelope.MessageType, err)
	}
	return nil
}
