package core

import (
	"context"
	"encoding/json"
	"testing"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/contracts/events"
	"cyber-deploy-hub/internal/domain"
)

func TestServiceStartsProvisioningSaga(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService("core-service", repo)
	payload := commands.RequestProvisionV1Payload{
		LabRunID:  "lab-1",
		StudentID: "student-1",
		CourseID:  "course-1",
		LabID:     "lab-template-1",
		Source:    "ui",
	}
	envelope := testEnvelope(t, contracts.MessageKindCommand, commands.RequestProvisionV1, payload)

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if repo.startedReq.LabRunID != payload.LabRunID {
		t.Fatalf("started request = %#v", repo.startedReq)
	}
	if repo.startedNext.MessageType != commands.ProjectAllocateV1.String() {
		t.Fatalf("next message type = %q", repo.startedNext.MessageType)
	}

	var nextPayload commands.ProjectAllocateV1Payload
	decodeTestPayload(t, repo.startedNext, &nextPayload)
	if nextPayload.StudentID != payload.StudentID || nextPayload.CourseID != payload.CourseID || nextPayload.LabID != payload.LabID {
		t.Fatalf("project allocate payload = %#v", nextPayload)
	}
}

func TestServiceAdvancesAfterProjectAllocated(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService("core-service", repo)
	envelope := testEnvelope(t, contracts.MessageKindEvent, events.ProjectAllocatedV1, events.ProjectAllocatedV1Payload{
		LabRunID:  "lab-1",
		ProjectID: "project-1",
		DomainID:  "domain-1",
	})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	transition := repo.lastTransition(t)
	if transition.State != domain.LabRunCheckingCapacity {
		t.Fatalf("state = %q", transition.State)
	}
	if transition.ProjectID != "project-1" {
		t.Fatalf("project id = %q", transition.ProjectID)
	}
	if len(transition.Next) != 1 || transition.Next[0].MessageType != commands.CapacityCheckV1.String() {
		t.Fatalf("next = %#v", transition.Next)
	}
	var nextPayload commands.CapacityCheckV1Payload
	decodeTestPayload(t, transition.Next[0], &nextPayload)
	if nextPayload.Resources.VCPU != 9 || nextPayload.Resources.RAMMiB != 16*1024 || nextPayload.Resources.DiskGiB != 214 {
		t.Fatalf("lab 3 resources = %#v", nextPayload.Resources)
	}
}

func TestServiceFailsAndReleasesProjectWhenCapacityDenied(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService("core-service", repo)
	envelope := testEnvelope(t, contracts.MessageKindEvent, events.CapacityDeniedV1, events.CapacityDecisionV1Payload{
		LabRunID:  "lab-1",
		ProjectID: "project-1",
		Approved:  false,
		Threshold: 90,
		Reason:    "predicted utilization is above 90%",
	})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	failure := repo.lastFailure(t)
	if failure.Code != "CAPACITY_DENIED" {
		t.Fatalf("failure code = %q", failure.Code)
	}
	if got := messageTypes(failure.Next); !sameStrings(got, []string{commands.ProjectReleaseV1.String(), events.LabFailedV1.String()}) {
		t.Fatalf("next message types = %#v", got)
	}
}

func TestServiceSchedulesCleanupWhenVDIAccessIssued(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService("core-service", repo)
	envelope := testEnvelope(t, contracts.MessageKindEvent, events.VDIAccessIssuedV1, events.VDIAccessIssuedV1Payload{
		LabRunID:  "lab-1",
		StudentID: "student-1",
		URL:       "https://vdi.example/lab-1",
		ExpiresAt: "2026-05-21T12:00:00Z",
	})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	transition := repo.lastTransition(t)
	if transition.State != domain.LabRunReady {
		t.Fatalf("state = %q", transition.State)
	}
	if transition.VDIURL != "https://vdi.example/lab-1" {
		t.Fatalf("vdi url = %q", transition.VDIURL)
	}
	if got := messageTypes(transition.Next); !sameStrings(got, []string{commands.LifecycleScheduleCleanupV1.String(), events.LabReadyV1.String()}) {
		t.Fatalf("next message types = %#v", got)
	}
}

func TestServiceCleansCloudAfterDeployFailure(t *testing.T) {
	repo := &fakeRepository{
		labRun: LabRun{
			ID:        "lab-1",
			ProjectID: "project-1",
			State:     domain.LabRunDeploying,
		},
	}
	service := NewService("core-service", repo)
	envelope := testEnvelope(t, contracts.MessageKindEvent, events.CloudDeployFailedV1, events.FailurePayload{
		LabRunID: "lab-1",
		Code:     "OPENSTACK_ERROR",
		Message:  "nova returned ERROR",
	})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	failure := repo.lastFailure(t)
	if failure.Code != "CLOUD_DEPLOY_FAILED" {
		t.Fatalf("failure code = %q", failure.Code)
	}
	if got := messageTypes(failure.Next); !sameStrings(got, []string{events.LabFailedV1.String(), commands.CloudCleanupLabV1.String()}) {
		t.Fatalf("next message types = %#v", got)
	}
}

type fakeRepository struct {
	startedReq     commands.RequestProvisionV1Payload
	startedCommand contracts.Envelope
	startedNext    contracts.Envelope
	transitions    []Transition
	failures       []Failure
	labRun         LabRun
}

func (r *fakeRepository) StartProvisioning(_ context.Context, req commands.RequestProvisionV1Payload, command contracts.Envelope, next contracts.Envelope) error {
	r.startedReq = req
	r.startedCommand = command
	r.startedNext = next
	return nil
}

func (r *fakeRepository) Advance(_ context.Context, transition Transition) error {
	r.transitions = append(r.transitions, transition)
	return nil
}

func (r *fakeRepository) Fail(_ context.Context, failure Failure) error {
	r.failures = append(r.failures, failure)
	return nil
}

func (r *fakeRepository) LoadLabRun(_ context.Context, labRunID string) (LabRun, error) {
	if r.labRun.ID == "" {
		return LabRun{
			ID:        labRunID,
			StudentID: "student-1",
			CourseID:  "course-1",
			LabID:     "lab-template-1",
			ProjectID: "project-1",
			State:     domain.LabRunDeploying,
		}, nil
	}
	return r.labRun, nil
}

func (r *fakeRepository) lastTransition(t *testing.T) Transition {
	t.Helper()
	if len(r.transitions) == 0 {
		t.Fatal("expected transition")
	}
	return r.transitions[len(r.transitions)-1]
}

func (r *fakeRepository) lastFailure(t *testing.T) Failure {
	t.Helper()
	if len(r.failures) == 0 {
		t.Fatal("expected failure")
	}
	return r.failures[len(r.failures)-1]
}

func testEnvelope(t *testing.T, kind contracts.MessageKind, subject contracts.Subject, payload any) contracts.Envelope {
	t.Helper()
	envelope, err := contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:           kind,
		Type:           subject,
		Producer:       "test-service",
		SagaID:         "saga-1",
		AggregateType:  aggregateTypeLabRun,
		AggregateID:    "lab-1",
		IdempotencyKey: "test:" + subject.String(),
		Payload:        payload,
	})
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	return envelope
}

func decodeTestPayload(t *testing.T, envelope contracts.Envelope, dst any) {
	t.Helper()
	if err := json.Unmarshal(envelope.Payload, dst); err != nil {
		t.Fatalf("decode payload: %v", err)
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
