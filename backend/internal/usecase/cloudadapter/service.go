package cloudadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/contracts/events"
)

type CloudProvider interface {
	Deploy(ctx context.Context, req DeployRequest) (DeployResult, error)
	Cleanup(ctx context.Context, deployment Deployment) error
}

type Repository interface {
	SaveDeploySuccess(ctx context.Context, command contracts.Envelope, req DeployRequest, result DeployResult, secret EncryptedSecret, event contracts.Envelope) error
	SaveDeployFailure(ctx context.Context, command contracts.Envelope, req DeployRequest, result DeployResult, reason string, event contracts.Envelope) error
	LoadDeployment(ctx context.Context, labRunID string) (Deployment, bool, error)
	SaveCleanupSuccess(ctx context.Context, command contracts.Envelope, labRunID string, event contracts.Envelope) error
	SaveCleanupFailure(ctx context.Context, command contracts.Envelope, labRunID string, reason string, event contracts.Envelope) error
}

type Service struct {
	producer          string
	provider          CloudProvider
	repo              Repository
	encryptor         PrivateKeyEncryptor
	defaultBlueprints []commands.VMBlueprint
}

func NewService(producer string, provider CloudProvider, repo Repository, encryptor PrivateKeyEncryptor, defaultBlueprints []commands.VMBlueprint) (*Service, error) {
	if strings.TrimSpace(producer) == "" {
		return nil, errors.New("producer is empty")
	}
	if provider == nil {
		return nil, errors.New("cloud provider is nil")
	}
	if repo == nil {
		return nil, errors.New("repository is nil")
	}
	if encryptor == nil {
		return nil, errors.New("private key encryptor is nil")
	}
	if err := validateBlueprints(defaultBlueprints); err != nil {
		return nil, err
	}
	return &Service{
		producer:          producer,
		provider:          provider,
		repo:              repo,
		encryptor:         encryptor,
		defaultBlueprints: append([]commands.VMBlueprint(nil), defaultBlueprints...),
	}, nil
}

func (s *Service) Handle(ctx context.Context, envelope contracts.Envelope) error {
	switch envelope.MessageType {
	case commands.CloudDeployVDIV1.String():
		return s.handleDeploy(ctx, envelope)
	case commands.CloudCleanupLabV1.String():
		return s.handleCleanup(ctx, envelope)
	default:
		return nil
	}
}

func (s *Service) handleDeploy(ctx context.Context, envelope contracts.Envelope) error {
	var payload commands.CloudDeployVDIV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	req := DeployRequest{
		LabRunID:  payload.LabRunID,
		ProjectID: payload.ProjectID,
		Instances: payload.Instances,
	}
	if len(req.Instances) == 0 {
		req.Instances = append([]commands.VMBlueprint(nil), s.defaultBlueprints...)
	}
	if err := validateDeployRequest(req); err != nil {
		return err
	}

	if err := s.encryptor.Ready(); err != nil {
		return s.saveDeployFailure(ctx, envelope, req, DeployResult{}, "PRIVATE_KEY_ENCRYPTION_UNAVAILABLE", err.Error())
	}

	result, err := s.provider.Deploy(ctx, req)
	if err != nil {
		partial := DeployResult{}
		var deployErr *DeployError
		if errors.As(err, &deployErr) {
			partial = deployErr.Result
		}
		return s.saveDeployFailure(ctx, envelope, req, partial, "CLOUD_DEPLOY_FAILED", err.Error())
	}

	secret, err := s.encryptor.Encrypt(result.PrivateKey)
	if err != nil {
		return s.saveDeployFailure(ctx, envelope, req, result, "PRIVATE_KEY_ENCRYPTION_FAILED", err.Error())
	}
	event, err := s.newEvent(envelope, events.CloudVDIDeployedV1, events.CloudVDIDeployedV1Payload{
		LabRunID:  req.LabRunID,
		ProjectID: req.ProjectID,
		Instances: deployedInstances(result.Instances),
	})
	if err != nil {
		return err
	}
	return s.repo.SaveDeploySuccess(ctx, envelope, req, result, secret, event)
}

func (s *Service) handleCleanup(ctx context.Context, envelope contracts.Envelope) error {
	var payload commands.CloudCleanupLabV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	if strings.TrimSpace(payload.LabRunID) == "" {
		return errors.New("lab_run_id is required")
	}

	deployment, found, err := s.repo.LoadDeployment(ctx, payload.LabRunID)
	if err != nil {
		return err
	}
	if !found {
		event, err := s.cleanedEvent(envelope, payload.LabRunID)
		if err != nil {
			return err
		}
		return s.repo.SaveCleanupSuccess(ctx, envelope, payload.LabRunID, event)
	}
	if err := s.provider.Cleanup(ctx, deployment); err != nil {
		event, newEventErr := s.failureEvent(envelope, events.CloudCleanupFailedV1, payload.LabRunID, "CLOUD_CLEANUP_FAILED", err.Error())
		if newEventErr != nil {
			return newEventErr
		}
		return s.repo.SaveCleanupFailure(ctx, envelope, payload.LabRunID, err.Error(), event)
	}
	event, err := s.cleanedEvent(envelope, payload.LabRunID)
	if err != nil {
		return err
	}
	return s.repo.SaveCleanupSuccess(ctx, envelope, payload.LabRunID, event)
}

func (s *Service) saveDeployFailure(ctx context.Context, envelope contracts.Envelope, req DeployRequest, result DeployResult, code string, message string) error {
	event, err := s.failureEvent(envelope, events.CloudDeployFailedV1, req.LabRunID, code, message)
	if err != nil {
		return err
	}
	return s.repo.SaveDeployFailure(ctx, envelope, req, result, message, event)
}

func (s *Service) cleanedEvent(cause contracts.Envelope, labRunID string) (contracts.Envelope, error) {
	return s.newEvent(cause, events.CloudLabCleanedV1, events.LabRunEventPayload{
		LabRunID: labRunID,
		State:    deploymentStateCleaned,
	})
}

func (s *Service) failureEvent(cause contracts.Envelope, subject contracts.Subject, labRunID string, code string, message string) (contracts.Envelope, error) {
	envelope, err := s.newEvent(cause, subject, events.FailurePayload{
		LabRunID: labRunID,
		Code:     code,
		Message:  message,
	})
	if err != nil {
		return contracts.Envelope{}, err
	}
	envelope.Error = &contracts.MessageError{Code: code, Message: message}
	return envelope, nil
}

func (s *Service) newEvent(cause contracts.Envelope, subject contracts.Subject, payload any) (contracts.Envelope, error) {
	envelope, err := contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:           contracts.MessageKindEvent,
		Type:           subject,
		Producer:       s.producer,
		CorrelationID:  cause.CorrelationID,
		CausationID:    cause.MessageID,
		SagaID:         cause.SagaID,
		AggregateType:  "lab_run",
		AggregateID:    cause.AggregateID,
		IdempotencyKey: cause.SagaID + ":" + subject.String(),
		Payload:        payload,
	})
	if err != nil {
		return contracts.Envelope{}, err
	}
	envelope.MessageID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(cause.MessageID+":"+subject.String())).String()
	return envelope, nil
}

func validateDeployRequest(req DeployRequest) error {
	if strings.TrimSpace(req.LabRunID) == "" {
		return errors.New("lab_run_id is required")
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		return errors.New("project_id is required")
	}
	if len(req.Instances) == 0 {
		return errors.New("instances are required")
	}
	return validateBlueprints(req.Instances)
}

func deployedInstances(instances []Instance) []events.DeployedInstance {
	result := make([]events.DeployedInstance, 0, len(instances))
	for _, instance := range instances {
		result = append(result, events.DeployedInstance{
			Name:       instance.Name,
			ServerID:   instance.ServerID,
			VolumeID:   instance.VolumeID,
			PortID:     instance.PortID,
			InternalIP: instance.FixedIP,
			AccessIP:   instance.AccessIP,
		})
	}
	return result
}

func decodePayload(envelope contracts.Envelope, dst any) error {
	if err := json.Unmarshal(envelope.Payload, dst); err != nil {
		return fmt.Errorf("decode %s payload: %w", envelope.MessageType, err)
	}
	return nil
}
