package projectpool

import (
	"context"
	"encoding/json"
	"testing"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
)

func TestServiceDelegatesProjectAllocateCommand(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)
	payload := commands.ProjectAllocateV1Payload{
		LabRunID:  "lab-1",
		StudentID: "student-1",
		CourseID:  "course-1",
		LabID:     "linux",
	}
	envelope := testEnvelope(t, commands.ProjectAllocateV1, payload)

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if repo.allocated.LabRunID != payload.LabRunID {
		t.Fatalf("allocated payload = %#v", repo.allocated)
	}
	if repo.allocateCommand.MessageID != envelope.MessageID {
		t.Fatalf("command was not forwarded")
	}
}

func TestServiceImportsSeedCommand(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)
	payload := commands.ProjectImportSeedV1Payload{
		Domains: []commands.ProjectPoolDomainV1{{
			DomainID: "domain-1",
			CourseID: "course-1",
			Name:     "Course domain",
		}},
		Projects: []commands.ProjectPoolProjectV1{{
			ProjectID: "11111111-1111-4111-8111-111111111111",
			DomainID:  "domain-1",
			Name:      "course-1-project-1",
		}},
	}
	envelope := testEnvelope(t, commands.ProjectImportSeedV1, payload)

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if repo.seed.Domains[0].CourseID != "course-1" || repo.seed.Projects[0].Name != "course-1-project-1" {
		t.Fatalf("seed = %#v", repo.seed)
	}
}

func TestServiceDelegatesProjectReleaseCommand(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)
	payload := commands.ProjectReleaseV1Payload{
		LabRunID:  "lab-1",
		ProjectID: "11111111-1111-4111-8111-111111111111",
		Reason:    "cloud_cleaned",
	}
	envelope := testEnvelope(t, commands.ProjectReleaseV1, payload)

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if repo.released.ProjectID != payload.ProjectID {
		t.Fatalf("released payload = %#v", repo.released)
	}
	if repo.releaseCommand.MessageID != envelope.MessageID {
		t.Fatalf("command was not forwarded")
	}
}

func TestServiceIgnoresUnrelatedMessages(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)
	envelope := testEnvelope(t, contracts.Subject("cmd.capacity.check.v1"), map[string]string{"lab_run_id": "lab-1"})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if repo.calls != 0 {
		t.Fatalf("unexpected repository calls = %d", repo.calls)
	}
}

func TestQuarantineReason(t *testing.T) {
	if !isQuarantineReason("cleanup_failed") {
		t.Fatal("cleanup_failed should quarantine project")
	}
	if !isQuarantineReason("manual quarantine after audit") {
		t.Fatal("quarantine reason should quarantine project")
	}
	if isQuarantineReason("capacity_denied") {
		t.Fatal("capacity_denied should release project to free pool")
	}
}

type fakeRepository struct {
	calls           int
	seed            Seed
	allocated       commands.ProjectAllocateV1Payload
	released        commands.ProjectReleaseV1Payload
	allocateCommand contracts.Envelope
	releaseCommand  contracts.Envelope
}

func (r *fakeRepository) ImportSeed(_ context.Context, seed Seed) error {
	r.calls++
	r.seed = seed
	return nil
}

func (r *fakeRepository) Allocate(_ context.Context, command contracts.Envelope, req commands.ProjectAllocateV1Payload) error {
	r.calls++
	r.allocateCommand = command
	r.allocated = req
	return nil
}

func (r *fakeRepository) Release(_ context.Context, command contracts.Envelope, req commands.ProjectReleaseV1Payload) error {
	r.calls++
	r.releaseCommand = command
	r.released = req
	return nil
}

func testEnvelope(t *testing.T, subject contracts.Subject, payload any) contracts.Envelope {
	t.Helper()
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return contracts.Envelope{
		MessageID:      "11111111-1111-4111-8111-111111111111",
		MessageKind:    contracts.MessageKindCommand,
		MessageType:    subject.String(),
		SchemaVersion:  1,
		Producer:       "test",
		CorrelationID:  "11111111-1111-4111-8111-111111111111",
		SagaID:         "22222222-2222-4222-8222-222222222222",
		AggregateType:  aggregateTypeLabRun,
		AggregateID:    "33333333-3333-4333-8333-333333333333",
		IdempotencyKey: "test:" + subject.String(),
		Payload:        rawPayload,
	}
}
