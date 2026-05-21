package settings

import (
	"context"
	"errors"
	"strings"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
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

type UpdateRequest struct {
	ChangedBy      string
	Values         map[string]any
	IdempotencyKey string
}

type UpdateAccepted struct {
	CommandID string `json:"command_id"`
	Status    string `json:"status"`
}

func NewService(producer string, bus Publisher, outbox Outbox) *Service {
	return &Service{producer: producer, bus: bus, outbox: outbox}
}

func (s *Service) Update(ctx context.Context, req UpdateRequest) (UpdateAccepted, error) {
	if strings.TrimSpace(req.ChangedBy) == "" {
		return UpdateAccepted{}, errors.New("changed_by is required")
	}
	if len(req.Values) == 0 {
		return UpdateAccepted{}, errors.New("values are required")
	}
	if s.bus == nil && s.outbox == nil {
		return UpdateAccepted{}, errors.New("command bus is not configured")
	}

	envelope, err := contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:           contracts.MessageKindCommand,
		Type:           commands.SettingsUpdateV1,
		Producer:       s.producer,
		AggregateType:  "settings",
		AggregateID:    "runtime",
		IdempotencyKey: req.IdempotencyKey,
		Payload: commands.SettingsUpdateV1Payload{
			ChangedBy: req.ChangedBy,
			Values:    req.Values,
		},
	})
	if err != nil {
		return UpdateAccepted{}, err
	}

	if s.outbox != nil {
		if err := s.outbox.Enqueue(ctx, envelope); err != nil {
			return UpdateAccepted{}, err
		}
	} else if err := s.bus.Publish(ctx, commands.SettingsUpdateV1.String(), envelope); err != nil {
		return UpdateAccepted{}, err
	}
	return UpdateAccepted{CommandID: envelope.MessageID, Status: "ACCEPTED"}, nil
}
