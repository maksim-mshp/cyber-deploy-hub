package checker

import (
	"context"
	"testing"
	"time"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/contracts/events"
)

func TestServicePublishesCompletedResult(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		profile: Profile{
			ID:      "default",
			Name:    "Default",
			SSHUser: "ubuntu",
			Steps: []Step{
				{Sequence: 1, Name: "command", Type: StepCommandExitCode, Command: "true", TimeoutSeconds: 1},
			},
		},
		target: Target{
			LabRunID:            "33333333-3333-4333-8333-333333333333",
			ProjectID:           "project-1",
			Host:                "10.0.0.5",
			Port:                22,
			EncryptedPrivateKey: []byte("cipher"),
			PrivateKeyNonce:     []byte("nonce"),
		},
	}
	service, err := NewService("checker-service", repo, fakeRunner{
		results: []StepResult{
			{Sequence: 1, Name: "command", Type: StepCommandExitCode, Passed: true, ExitCode: 0, Message: "passed", StartedAt: time.Now(), FinishedAt: time.Now()},
		},
	}, fakeDecryptor{}, 22)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	envelope := testEnvelope(t, commands.CheckerRunV1, commands.CheckerRunV1Payload{
		LabRunID:  "33333333-3333-4333-8333-333333333333",
		ProfileID: "default",
	})
	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if repo.completed.State != runStatePassed {
		t.Fatalf("state = %s, want %s", repo.completed.State, runStatePassed)
	}
	if !repo.completed.Passed {
		t.Fatal("completed run is not marked as passed")
	}
	if repo.completedEvent.MessageType != events.CheckerCompletedV1.String() {
		t.Fatalf("event = %s, want %s", repo.completedEvent.MessageType, events.CheckerCompletedV1)
	}
}

func TestServicePublishesFailureWhenProfileMissing(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{profileFound: false}
	service, err := NewService("checker-service", repo, fakeRunner{}, fakeDecryptor{}, 22)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	envelope := testEnvelope(t, commands.CheckerRunV1, commands.CheckerRunV1Payload{
		LabRunID:  "33333333-3333-4333-8333-333333333333",
		ProfileID: "missing",
	})
	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if repo.failed.State != runStateError {
		t.Fatalf("state = %s, want %s", repo.failed.State, runStateError)
	}
	if repo.failed.ErrorCode != "CHECK_PROFILE_NOT_FOUND" {
		t.Fatalf("error code = %s", repo.failed.ErrorCode)
	}
	if repo.failedEvent.MessageType != events.CheckerFailedV1.String() {
		t.Fatalf("event = %s, want %s", repo.failedEvent.MessageType, events.CheckerFailedV1)
	}
}

func TestServiceRunsCustomProfileFromCommand(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		target: Target{
			LabRunID:            "33333333-3333-4333-8333-333333333333",
			ProjectID:           "project-1",
			Host:                "10.0.0.5",
			Port:                22,
			EncryptedPrivateKey: []byte("cipher"),
			PrivateKeyNonce:     []byte("nonce"),
		},
	}
	service, err := NewService("checker-service", repo, fakeRunner{
		results: []StepResult{{
			Sequence:   1,
			Name:       "filesystem",
			Type:       StepCommandExitCode,
			Passed:     true,
			ExitCode:   0,
			Message:    "passed",
			StartedAt:  time.Now(),
			FinishedAt: time.Now(),
		}},
	}, fakeDecryptor{}, 22)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	envelope := testEnvelope(t, commands.CheckerRunV1, commands.CheckerRunV1Payload{
		LabRunID: "33333333-3333-4333-8333-333333333333",
		Profile: &commands.CheckerProfileV1{
			ID:      "teacher-storage",
			Name:    "Storage check",
			SSHUser: "ubuntu",
			Steps: []commands.CheckerStepV1{{
				Name:           "filesystem",
				Type:           "command_exit_code",
				Command:        "findmnt -n /",
				TimeoutSeconds: 10,
			}},
		},
	})
	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if repo.completed.ProfileID != "teacher-storage" || !repo.completed.Passed {
		t.Fatalf("completed run = %#v", repo.completed)
	}
}

type fakeRepository struct {
	profile        Profile
	profileFound   bool
	target         Target
	completed      RunRecord
	completedEvent contracts.Envelope
	failed         RunRecord
	failedEvent    contracts.Envelope
}

func (r *fakeRepository) LoadProfile(context.Context, string) (Profile, bool, error) {
	if !r.profileFound && r.profile.ID == "" {
		return Profile{}, false, nil
	}
	return r.profile, true, nil
}

func (r *fakeRepository) LoadTarget(context.Context, string, int) (Target, bool, error) {
	return r.target, true, nil
}

func (r *fakeRepository) SaveCompleted(_ context.Context, _ contracts.Envelope, run RunRecord, event contracts.Envelope) error {
	r.completed = run
	r.completedEvent = event
	return nil
}

func (r *fakeRepository) SaveFailed(_ context.Context, _ contracts.Envelope, run RunRecord, event contracts.Envelope) error {
	r.failed = run
	r.failedEvent = event
	return nil
}

type fakeRunner struct {
	results []StepResult
	err     error
}

func (r fakeRunner) Run(context.Context, Profile, RemoteTarget) ([]StepResult, error) {
	return r.results, r.err
}

type fakeDecryptor struct{}

func (fakeDecryptor) Ready() error {
	return nil
}

func (fakeDecryptor) Decrypt([]byte, []byte, string) ([]byte, error) {
	return []byte("private-key"), nil
}

func testEnvelope(t *testing.T, subject contracts.Subject, payload any) contracts.Envelope {
	t.Helper()
	envelope, err := contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:          contracts.MessageKindCommand,
		Type:          subject,
		Producer:      "test",
		CorrelationID: "correlation",
		SagaID:        "saga",
		AggregateType: "lab_run",
		AggregateID:   "33333333-3333-4333-8333-333333333333",
		Payload:       payload,
	})
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	return envelope
}
