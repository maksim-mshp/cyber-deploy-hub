package cloudadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/contracts/events"
)

func TestServiceDeploysAndEmitsCloudDeployed(t *testing.T) {
	repo := &fakeRepository{}
	provider := &fakeProvider{deployResult: DeployResult{
		KeyPairName: "cdh-key",
		PrivateKey:  []byte("private"),
		Instances: []Instance{{
			Name:     "vm-1",
			ImageID:  "image-1",
			FlavorID: "flavor-1",
			FixedIP:  "10.0.0.10",
			DiskGiB:  10,
			ServerID: "server-1",
			VolumeID: "volume-1",
			PortID:   "port-1",
			State:    "ACTIVE",
		}},
	}}
	service := newTestService(t, provider, repo, StaticEncryptor{Secret: EncryptedSecret{
		Ciphertext: []byte("cipher"),
		Nonce:      []byte("nonce"),
		KeyID:      "test",
	}}, nil)

	envelope := testEnvelope(t, commands.CloudDeployVDIV1, commands.CloudDeployVDIV1Payload{
		LabRunID:  "11111111-1111-4111-8111-111111111111",
		ProjectID: "project-1",
		Instances: []commands.VMBlueprint{{
			Name:     "vm-1",
			ImageID:  "image-1",
			FlavorID: "flavor-1",
			DiskGiB:  10,
		}},
	})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if repo.savedSuccess.LabRunID == "" {
		t.Fatal("deploy success was not saved")
	}
	if repo.event.MessageType != events.CloudVDIDeployedV1.String() {
		t.Fatalf("event type = %s", repo.event.MessageType)
	}
	var payload events.CloudVDIDeployedV1Payload
	if err := json.Unmarshal(repo.event.Payload, &payload); err != nil {
		t.Fatalf("decode event payload: %v", err)
	}
	if len(payload.Instances) != 1 || payload.Instances[0].ServerID != "server-1" {
		t.Fatalf("payload instances = %#v", payload.Instances)
	}
}

func TestServiceDeployFailurePersistsPartialResources(t *testing.T) {
	repo := &fakeRepository{}
	partial := DeployResult{
		KeyPairName: "cdh-key",
		PrivateKey:  []byte("private"),
		NetworkID:   "network-1",
		SubnetID:    "subnet-1",
		Instances: []Instance{{
			Name:     "vm-1",
			ImageID:  "image-1",
			FlavorID: "flavor-1",
			DiskGiB:  10,
			PortID:   "port-1",
			State:    "CREATING",
		}},
	}
	service := newTestService(t, &fakeProvider{
		deployErr: &DeployError{Result: partial, Err: errors.New("nova failed")},
	}, repo, StaticEncryptor{Secret: EncryptedSecret{Ciphertext: []byte("cipher")}}, []commands.VMBlueprint{{
		Name:     "vm-1",
		ImageID:  "image-1",
		FlavorID: "flavor-1",
		DiskGiB:  10,
	}})

	envelope := testEnvelope(t, commands.CloudDeployVDIV1, commands.CloudDeployVDIV1Payload{
		LabRunID:  "11111111-1111-4111-8111-111111111111",
		ProjectID: "project-1",
	})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if repo.savedFailure.LabRunID == "" {
		t.Fatal("deploy failure was not saved")
	}
	if len(repo.failureResult.Instances) != 1 || repo.failureResult.Instances[0].PortID != "port-1" {
		t.Fatalf("failure partial result = %#v", repo.failureResult)
	}
	if repo.failureResult.NetworkID != "network-1" || repo.failureResult.SubnetID != "subnet-1" {
		t.Fatalf("failure network result = %#v", repo.failureResult)
	}
	if repo.event.MessageType != events.CloudDeployFailedV1.String() || repo.event.Error == nil {
		t.Fatalf("failure event = %#v", repo.event)
	}
}

func TestServiceCleanupWithoutDeploymentStillEmitsCleaned(t *testing.T) {
	repo := &fakeRepository{}
	service := newTestService(t, &fakeProvider{}, repo, StaticEncryptor{Secret: EncryptedSecret{Ciphertext: []byte("cipher")}}, nil)
	envelope := testEnvelope(t, commands.CloudCleanupLabV1, commands.CloudCleanupLabV1Payload{
		LabRunID:  "11111111-1111-4111-8111-111111111111",
		ProjectID: "project-1",
		Reason:    "test",
	})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if repo.cleanedLabRunID == "" {
		t.Fatal("cleanup success was not saved")
	}
	if repo.event.MessageType != events.CloudLabCleanedV1.String() {
		t.Fatalf("event type = %s", repo.event.MessageType)
	}
}

type fakeProvider struct {
	deployResult DeployResult
	deployErr    error
	cleanupErr   error
	cleaned      Deployment
}

func (p *fakeProvider) Deploy(context.Context, DeployRequest) (DeployResult, error) {
	if p.deployErr != nil {
		return DeployResult{}, p.deployErr
	}
	return p.deployResult, nil
}

func (p *fakeProvider) Cleanup(_ context.Context, deployment Deployment) error {
	p.cleaned = deployment
	return p.cleanupErr
}

type fakeRepository struct {
	savedSuccess    DeployRequest
	savedFailure    DeployRequest
	failureResult   DeployResult
	deployment      Deployment
	hasDeployment   bool
	cleanedLabRunID string
	event           contracts.Envelope
}

func (r *fakeRepository) SaveDeploySuccess(_ context.Context, _ contracts.Envelope, req DeployRequest, _ DeployResult, _ EncryptedSecret, event contracts.Envelope) error {
	r.savedSuccess = req
	r.event = event
	return nil
}

func (r *fakeRepository) SaveDeployFailure(_ context.Context, _ contracts.Envelope, req DeployRequest, result DeployResult, _ string, event contracts.Envelope) error {
	r.savedFailure = req
	r.failureResult = result
	r.event = event
	return nil
}

func (r *fakeRepository) LoadDeployment(context.Context, string) (Deployment, bool, error) {
	return r.deployment, r.hasDeployment, nil
}

func (r *fakeRepository) SaveCleanupSuccess(_ context.Context, _ contracts.Envelope, labRunID string, event contracts.Envelope) error {
	r.cleanedLabRunID = labRunID
	r.event = event
	return nil
}

func (r *fakeRepository) SaveCleanupFailure(_ context.Context, _ contracts.Envelope, labRunID string, _ string, event contracts.Envelope) error {
	r.cleanedLabRunID = labRunID
	r.event = event
	return nil
}

func newTestService(t *testing.T, provider CloudProvider, repo Repository, encryptor PrivateKeyEncryptor, blueprints []commands.VMBlueprint) *Service {
	t.Helper()
	service, err := NewService("cloud-adapter-service", provider, repo, encryptor, blueprints)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

type StaticEncryptor struct {
	Secret EncryptedSecret
	Err    error
}

func (e StaticEncryptor) Ready() error {
	return e.Err
}

func (e StaticEncryptor) Encrypt([]byte) (EncryptedSecret, error) {
	if e.Err != nil {
		return EncryptedSecret{}, e.Err
	}
	if len(e.Secret.Ciphertext) == 0 {
		return EncryptedSecret{}, fmt.Errorf("static encryptor has empty ciphertext")
	}
	return e.Secret, nil
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
