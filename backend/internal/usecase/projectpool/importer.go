package projectpool

import (
	"context"
	"errors"
	"strings"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
)

type SeedPublisher interface {
	Publish(ctx context.Context, subject string, envelope contracts.Envelope) error
}

type SeedOutbox interface {
	Enqueue(ctx context.Context, envelope contracts.Envelope) error
}

type SeedImporter struct {
	producer string
	bus      SeedPublisher
	outbox   SeedOutbox
}

type ImportRequest struct {
	Seed           Seed
	IdempotencyKey string
}

type ImportAccepted struct {
	CommandID string `json:"command_id"`
	Status    string `json:"status"`
}

func NewSeedImporter(producer string, bus SeedPublisher, outbox SeedOutbox) *SeedImporter {
	return &SeedImporter{producer: producer, bus: bus, outbox: outbox}
}

func (s *SeedImporter) RequestImport(ctx context.Context, req ImportRequest) (ImportAccepted, error) {
	if s == nil || (s.bus == nil && s.outbox == nil) {
		return ImportAccepted{}, errors.New("command bus is not configured")
	}
	if err := req.Seed.Validate(); err != nil {
		return ImportAccepted{}, err
	}
	if req.Seed.Empty() {
		return ImportAccepted{}, errors.New("project pool seed is empty")
	}

	envelope, err := contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:           contracts.MessageKindCommand,
		Type:           commands.ProjectImportSeedV1,
		Producer:       s.producer,
		AggregateType:  "project_pool",
		AggregateID:    seedAggregateID(req.Seed),
		IdempotencyKey: req.IdempotencyKey,
		Payload:        seedCommand(req.Seed),
	})
	if err != nil {
		return ImportAccepted{}, err
	}
	if s.outbox != nil {
		if err := s.outbox.Enqueue(ctx, envelope); err != nil {
			return ImportAccepted{}, err
		}
	} else if err := s.bus.Publish(ctx, commands.ProjectImportSeedV1.String(), envelope); err != nil {
		return ImportAccepted{}, err
	}
	return ImportAccepted{CommandID: envelope.MessageID, Status: "ACCEPTED"}, nil
}

func seedAggregateID(seed Seed) string {
	if len(seed.Domains) > 0 {
		if courseID := strings.TrimSpace(seed.Domains[0].CourseID); courseID != "" {
			return courseID
		}
	}
	return "seed"
}

func seedCommand(seed Seed) commands.ProjectImportSeedV1Payload {
	payload := commands.ProjectImportSeedV1Payload{
		Domains:  make([]commands.ProjectPoolDomainV1, 0, len(seed.Domains)),
		Projects: make([]commands.ProjectPoolProjectV1, 0, len(seed.Projects)),
	}
	for _, domain := range seed.Domains {
		payload.Domains = append(payload.Domains, commands.ProjectPoolDomainV1{
			DomainID: domain.DomainID,
			CourseID: domain.CourseID,
			Name:     domain.Name,
		})
	}
	for _, project := range seed.Projects {
		payload.Projects = append(payload.Projects, commands.ProjectPoolProjectV1{
			ProjectID: project.ProjectID,
			DomainID:  project.DomainID,
			Name:      project.Name,
		})
	}
	return payload
}
