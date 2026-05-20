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
	IdempotencyKey string
}

type ProvisionAccepted struct {
	LabRunID  string             `json:"lab_run_id"`
	SagaID    string             `json:"saga_id"`
	CommandID string             `json:"command_id"`
	Status    domain.LabRunState `json:"status"`
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

func (r RequestProvision) source() string {
	source := strings.TrimSpace(r.Source)
	if source == "" {
		return "ui"
	}
	return source
}
