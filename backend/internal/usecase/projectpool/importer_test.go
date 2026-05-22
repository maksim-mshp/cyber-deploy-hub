package projectpool

import (
	"context"
	"testing"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
)

func TestSeedImporterQueuesProjectImportCommand(t *testing.T) {
	outbox := &seedOutbox{}
	importer := NewSeedImporter("api-gateway-service", nil, outbox)

	result, err := importer.RequestImport(context.Background(), ImportRequest{
		Seed: Seed{
			Domains: []SeedDomain{{DomainID: "domain-1", CourseID: "course-1", Name: "Course domain"}},
			Projects: []SeedProject{{
				ProjectID: "11111111-1111-4111-8111-111111111111",
				DomainID:  "domain-1",
				Name:      "course-1-project-1",
			}},
		},
		IdempotencyKey: "seed:course-1",
	})
	if err != nil {
		t.Fatalf("RequestImport: %v", err)
	}
	if result.Status != "ACCEPTED" || result.CommandID == "" {
		t.Fatalf("result = %#v", result)
	}
	if outbox.envelope.MessageType != commands.ProjectImportSeedV1.String() {
		t.Fatalf("message type = %q", outbox.envelope.MessageType)
	}
	if outbox.envelope.AggregateType != "project_pool" || outbox.envelope.AggregateID != "course-1" {
		t.Fatalf("aggregate = %s/%s", outbox.envelope.AggregateType, outbox.envelope.AggregateID)
	}
}

type seedOutbox struct {
	envelope contracts.Envelope
}

func (o *seedOutbox) Enqueue(_ context.Context, envelope contracts.Envelope) error {
	o.envelope = envelope
	return nil
}
