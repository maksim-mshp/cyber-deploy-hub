package capacity

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/contracts/events"
)

func TestServiceApprovesCapacityCheck(t *testing.T) {
	repo := &fakeRepository{}
	service := newTestService(t, staticProvider{snapshot: Snapshot{
		Source:          "test",
		VCPUsTotal:      100,
		VCPUsFree:       80,
		RAMMiBTotal:     1000,
		RAMMiBFree:      800,
		StorageGiBTotal: 1000,
		StorageGiBUsed:  100,
	}}, repo, 90)
	envelope := testEnvelope(t, commands.CapacityCheckV1, commands.CapacityCheckV1Payload{
		LabRunID:  "lab-1",
		ProjectID: "project-1",
		Resources: commands.LabResourceProfile{
			VCPU:    10,
			RAMMiB:  100,
			DiskGiB: 100,
		},
	})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if !repo.decision.Approved {
		t.Fatalf("decision denied: %s", repo.decision.Reason)
	}
	if got := messageTypes(repo.next); !sameStrings(got, []string{events.CapacityApprovedV1.String()}) {
		t.Fatalf("next messages = %#v", got)
	}
}

func TestServiceDeniesAndEmitsAuditCommand(t *testing.T) {
	repo := &fakeRepository{}
	service := newTestService(t, staticProvider{snapshot: Snapshot{
		Source:          "test",
		VCPUsTotal:      100,
		VCPUsFree:       20,
		RAMMiBTotal:     1000,
		RAMMiBFree:      900,
		StorageGiBTotal: 1000,
		StorageGiBUsed:  100,
	}}, repo, 90)
	envelope := testEnvelope(t, commands.CapacityCheckV1, commands.CapacityCheckV1Payload{
		LabRunID:  "lab-1",
		ProjectID: "project-1",
		Resources: commands.LabResourceProfile{
			VCPU:    15,
			RAMMiB:  100,
			DiskGiB: 100,
		},
	})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if repo.decision.Approved {
		t.Fatal("expected denial")
	}
	if got := messageTypes(repo.next); !sameStrings(got, []string{events.CapacityDeniedV1.String(), commands.AuditWriteV1.String()}) {
		t.Fatalf("next messages = %#v", got)
	}
	if repo.next[0].Error == nil || repo.next[0].Error.Code != "CAPACITY_DENIED" {
		t.Fatalf("denied error = %#v", repo.next[0].Error)
	}
}

func TestServiceDeniesWhenStatProviderFails(t *testing.T) {
	repo := &fakeRepository{}
	service := newTestService(t, failingProvider{}, repo, 90)
	envelope := testEnvelope(t, commands.CapacityCheckV1, commands.CapacityCheckV1Payload{
		LabRunID:  "lab-1",
		ProjectID: "project-1",
		Resources: commands.LabResourceProfile{
			VCPU:    1,
			RAMMiB:  1,
			DiskGiB: 1,
		},
	})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if repo.decision.Approved {
		t.Fatal("expected denial")
	}
	if repo.snapshot.Source != "unavailable" {
		t.Fatalf("snapshot source = %q", repo.snapshot.Source)
	}
}

type staticProvider struct {
	snapshot Snapshot
}

func (p staticProvider) Current(context.Context) (Snapshot, error) {
	return p.snapshot, nil
}

type failingProvider struct{}

func (failingProvider) Current(context.Context) (Snapshot, error) {
	return Snapshot{}, errors.New("ki unavailable")
}

type fakeRepository struct {
	command  contracts.Envelope
	req      commands.CapacityCheckV1Payload
	snapshot Snapshot
	decision Decision
	next     []contracts.Envelope
	released commands.LabRunCommandPayload
}

func (r *fakeRepository) SaveCheck(_ context.Context, command contracts.Envelope, req commands.CapacityCheckV1Payload, snapshot Snapshot, decision Decision, next []contracts.Envelope) error {
	r.command = command
	r.req = req
	r.snapshot = snapshot
	r.decision = decision
	r.next = next
	return nil
}

func (r *fakeRepository) ReleaseReservation(_ context.Context, _ contracts.Envelope, req commands.LabRunCommandPayload, next contracts.Envelope) error {
	r.released = req
	r.next = []contracts.Envelope{next}
	return nil
}

func newTestService(t *testing.T, provider StatProvider, repo Repository, threshold float64) *Service {
	t.Helper()
	service, err := NewService("capacity-service", provider, NoopQuotaChecker{}, repo, threshold)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
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
		AggregateType:  "lab_run",
		AggregateID:    "33333333-3333-4333-8333-333333333333",
		IdempotencyKey: "test:" + subject.String(),
		Payload:        rawPayload,
	}
}

func messageTypes(envelopes []contracts.Envelope) []string {
	types := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		types = append(types, envelope.MessageType)
	}
	return types
}

func sameStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
