package labs

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/domain"
)

type Publisher interface {
	Publish(ctx context.Context, subject string, envelope contracts.Envelope) error
}

type Outbox interface {
	Enqueue(ctx context.Context, envelope contracts.Envelope) error
}

type Service struct {
	producer string
	bus      Publisher
	outbox   Outbox
}

type RequestProvision struct {
	StudentID      string
	CourseID       string
	LabID          string
	Source         string
	Resources      commands.LabResourceProfile
	Instances      []commands.VMBlueprint
	IdempotencyKey string
}

type ProvisionAccepted struct {
	LabRunID  string             `json:"lab_run_id"`
	SagaID    string             `json:"saga_id"`
	CommandID string             `json:"command_id"`
	Status    domain.LabRunState `json:"status"`
}

type LabCommand struct {
	LabRunID       string
	Reason         string
	IdempotencyKey string
}

type CheckCommand struct {
	LabRunID       string
	ProfileID      string
	Profile        *commands.CheckerProfileV1
	IdempotencyKey string
}

type CommandAccepted struct {
	CommandID string `json:"command_id"`
	Status    string `json:"status"`
}

func NewService(producer string, bus Publisher, outbox Outbox) *Service {
	return &Service{producer: producer, bus: bus, outbox: outbox}
}

func (s *Service) RequestProvision(ctx context.Context, req RequestProvision) (ProvisionAccepted, error) {
	if err := req.validate(); err != nil {
		return ProvisionAccepted{}, err
	}
	if s.bus == nil && s.outbox == nil {
		return ProvisionAccepted{}, errors.New("command bus is not configured")
	}

	labRunID := uuid.NewString()
	sagaID := uuid.NewString()
	payload := commands.RequestProvisionV1Payload{
		LabRunID:  labRunID,
		StudentID: req.StudentID,
		CourseID:  req.CourseID,
		LabID:     req.LabID,
		Source:    req.source(),
		Resources: req.Resources,
		Instances: append([]commands.VMBlueprint(nil), req.Instances...),
	}

	envelope, err := contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:           contracts.MessageKindCommand,
		Type:           commands.RequestProvisionV1,
		Producer:       s.producer,
		SagaID:         sagaID,
		AggregateType:  "lab_run",
		AggregateID:    labRunID,
		IdempotencyKey: req.IdempotencyKey,
		Payload:        payload,
	})
	if err != nil {
		return ProvisionAccepted{}, err
	}

	if s.outbox != nil {
		if err := s.outbox.Enqueue(ctx, envelope); err != nil {
			return ProvisionAccepted{}, err
		}
	} else if err := s.bus.Publish(ctx, commands.RequestProvisionV1.String(), envelope); err != nil {
		return ProvisionAccepted{}, err
	}

	return ProvisionAccepted{
		LabRunID:  labRunID,
		SagaID:    sagaID,
		CommandID: envelope.MessageID,
		Status:    domain.LabRunRequested,
	}, nil
}

func (s *Service) RequestFreeze(ctx context.Context, req LabCommand) (CommandAccepted, error) {
	if err := req.validate(); err != nil {
		return CommandAccepted{}, err
	}
	return s.publishCommand(ctx, commands.RequestFreezeV1, req.IdempotencyKey, commands.LabRunCommandPayload{
		LabRunID: req.LabRunID,
		Reason:   req.Reason,
	})
}

func (s *Service) RequestCleanup(ctx context.Context, req LabCommand) (CommandAccepted, error) {
	if err := req.validate(); err != nil {
		return CommandAccepted{}, err
	}
	return s.publishCommand(ctx, commands.RequestCleanupV1, req.IdempotencyKey, commands.LabRunCommandPayload{
		LabRunID: req.LabRunID,
		Reason:   req.Reason,
	})
}

func (s *Service) RequestCheck(ctx context.Context, req CheckCommand) (CommandAccepted, error) {
	if strings.TrimSpace(req.LabRunID) == "" {
		return CommandAccepted{}, errors.New("lab_run_id is required")
	}
	profileID := strings.TrimSpace(req.ProfileID)
	profile := req.Profile
	if profile != nil {
		nextProfile := *profile
		nextProfile.ID = strings.TrimSpace(nextProfile.ID)
		if nextProfile.ID != "" {
			profileID = nextProfile.ID
		}
		profile = &nextProfile
	}
	if profileID == "" {
		profileID = "default"
	}
	return s.publishCommand(ctx, commands.RequestVerificationV1, req.IdempotencyKey, commands.RequestVerificationV1Payload{
		LabRunID:  req.LabRunID,
		ProfileID: profileID,
		Profile:   profile,
	})
}

func (s *Service) publishCommand(ctx context.Context, subject contracts.Subject, idempotencyKey string, payload any) (CommandAccepted, error) {
	if s.bus == nil && s.outbox == nil {
		return CommandAccepted{}, errors.New("command bus is not configured")
	}
	labRunID := payloadLabRunID(payload)
	envelope, err := contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:           contracts.MessageKindCommand,
		Type:           subject,
		Producer:       s.producer,
		AggregateType:  "lab_run",
		AggregateID:    labRunID,
		IdempotencyKey: idempotencyKey,
		Payload:        payload,
	})
	if err != nil {
		return CommandAccepted{}, err
	}
	if s.outbox != nil {
		if err := s.outbox.Enqueue(ctx, envelope); err != nil {
			return CommandAccepted{}, err
		}
	} else if err := s.bus.Publish(ctx, subject.String(), envelope); err != nil {
		return CommandAccepted{}, err
	}
	return CommandAccepted{CommandID: envelope.MessageID, Status: "ACCEPTED"}, nil
}

func (r RequestProvision) validate() error {
	if strings.TrimSpace(r.StudentID) == "" {
		return errors.New("student_id is required")
	}
	if strings.TrimSpace(r.CourseID) == "" {
		return errors.New("course_id is required")
	}
	if strings.TrimSpace(r.LabID) == "" {
		return errors.New("lab_id is required")
	}
	return nil
}

func (r LabCommand) validate() error {
	if strings.TrimSpace(r.LabRunID) == "" {
		return errors.New("lab_run_id is required")
	}
	return nil
}

func payloadLabRunID(payload any) string {
	switch typed := payload.(type) {
	case commands.LabRunCommandPayload:
		return typed.LabRunID
	case commands.RequestVerificationV1Payload:
		return typed.LabRunID
	default:
		return ""
	}
}

func (r RequestProvision) source() string {
	source := strings.TrimSpace(r.Source)
	if source == "" {
		return "ui"
	}
	return source
}
