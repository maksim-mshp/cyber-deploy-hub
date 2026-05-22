package checker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/contracts/events"
)

type Repository interface {
	LoadProfile(ctx context.Context, profileID string) (Profile, bool, error)
	LoadTarget(ctx context.Context, labRunID string, defaultPort int) (Target, bool, error)
	SaveCompleted(ctx context.Context, command contracts.Envelope, run RunRecord, event contracts.Envelope) error
	SaveFailed(ctx context.Context, command contracts.Envelope, run RunRecord, event contracts.Envelope) error
}

type PrivateKeyDecryptor interface {
	Ready() error
	Decrypt(ciphertext []byte, nonce []byte, keyID string) ([]byte, error)
}

type Service struct {
	producer    string
	repo        Repository
	runner      Runner
	decryptor   PrivateKeyDecryptor
	defaultPort int
}

func NewService(producer string, repo Repository, runner Runner, decryptor PrivateKeyDecryptor, defaultPort int) (*Service, error) {
	if strings.TrimSpace(producer) == "" {
		return nil, errors.New("producer is empty")
	}
	if repo == nil {
		return nil, errors.New("repository is nil")
	}
	if runner == nil {
		return nil, errors.New("runner is nil")
	}
	if decryptor == nil {
		return nil, errors.New("private key decryptor is nil")
	}
	if defaultPort <= 0 {
		defaultPort = 22
	}
	return &Service{producer: producer, repo: repo, runner: runner, decryptor: decryptor, defaultPort: defaultPort}, nil
}

func (s *Service) Handle(ctx context.Context, envelope contracts.Envelope) error {
	switch envelope.MessageType {
	case commands.CheckerRunV1.String():
		return s.handleRun(ctx, envelope)
	default:
		return nil
	}
}

func (s *Service) handleRun(ctx context.Context, envelope contracts.Envelope) error {
	started := time.Now().UTC()
	var payload commands.CheckerRunV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	if strings.TrimSpace(payload.LabRunID) == "" {
		return errors.New("lab_run_id is required")
	}
	profileID := requestedProfileID(payload)

	run := RunRecord{
		ID:        uuid.NewSHA1(uuid.NameSpaceURL, []byte(envelope.MessageID+":checker-run")).String(),
		LabRunID:  payload.LabRunID,
		ProfileID: profileID,
		StartedAt: started,
	}

	profile, found, err := s.loadProfile(ctx, payload, profileID)
	if err != nil {
		if payload.Profile != nil && len(payload.Profile.Steps) > 0 {
			return s.saveFailed(ctx, envelope, run, "CHECK_PROFILE_INVALID", err.Error())
		}
		return err
	}
	if !found {
		return s.saveFailed(ctx, envelope, run, "CHECK_PROFILE_NOT_FOUND", fmt.Sprintf("checker profile %q was not found", profileID))
	}
	target, found, err := s.repo.LoadTarget(ctx, payload.LabRunID, s.defaultPort)
	if err != nil {
		return err
	}
	if !found {
		return s.saveFailed(ctx, envelope, run, "CHECK_TARGET_NOT_FOUND", "cloud deployment was not found for lab run")
	}
	if strings.TrimSpace(target.Host) == "" {
		return s.saveFailed(ctx, envelope, run, "CHECK_TARGET_HOST_EMPTY", "cloud deployment does not include a VM fixed IP")
	}
	if len(target.EncryptedPrivateKey) == 0 || len(target.PrivateKeyNonce) == 0 {
		return s.saveFailed(ctx, envelope, run, "CHECK_PRIVATE_KEY_MISSING", "encrypted private key is missing")
	}
	if err := s.decryptor.Ready(); err != nil {
		return s.saveFailed(ctx, envelope, run, "CHECK_PRIVATE_KEY_DECRYPTOR_UNAVAILABLE", err.Error())
	}
	privateKey, err := s.decryptor.Decrypt(target.EncryptedPrivateKey, target.PrivateKeyNonce, target.PrivateKeyKeyID)
	if err != nil {
		return s.saveFailed(ctx, envelope, run, "CHECK_PRIVATE_KEY_DECRYPT_FAILED", err.Error())
	}

	results, err := s.runner.Run(ctx, profile, RemoteTarget{
		Host:       target.Host,
		Port:       target.Port,
		User:       profile.SSHUser,
		PrivateKey: privateKey,
	})
	if err != nil {
		run.Results = results
		return s.saveFailed(ctx, envelope, run, "CHECK_SSH_RUN_FAILED", err.Error())
	}

	passed := allPassed(results) && len(results) == len(profile.Steps)
	run.Results = results
	run.Passed = passed
	if passed {
		run.State = runStatePassed
	} else {
		run.State = runStateFailed
	}
	run.FinishedAt = time.Now().UTC()
	event, err := s.newEvent(envelope, events.CheckerCompletedV1, events.CheckerCompletedV1Payload{
		LabRunID: payload.LabRunID,
		Passed:   passed,
		Results:  eventResults(results),
	})
	if err != nil {
		return err
	}
	return s.repo.SaveCompleted(ctx, envelope, run, event)
}

func (s *Service) loadProfile(ctx context.Context, payload commands.CheckerRunV1Payload, profileID string) (Profile, bool, error) {
	if payload.Profile == nil || len(payload.Profile.Steps) == 0 {
		return s.repo.LoadProfile(ctx, profileID)
	}
	profile := commandProfile(*payload.Profile)
	if strings.TrimSpace(profile.ID) == "" {
		profile.ID = profileID
	}
	normalizeProfile(&profile, profile.SSHUser)
	if err := validateProfile(profile); err != nil {
		return Profile{}, false, err
	}
	return profile, true, nil
}

func requestedProfileID(payload commands.CheckerRunV1Payload) string {
	profileID := strings.TrimSpace(payload.ProfileID)
	if profileID == "" && payload.Profile != nil {
		profileID = strings.TrimSpace(payload.Profile.ID)
	}
	if profileID == "" {
		return "default"
	}
	return profileID
}

func commandProfile(payload commands.CheckerProfileV1) Profile {
	profile := Profile{
		ID:      payload.ID,
		Name:    payload.Name,
		SSHUser: payload.SSHUser,
		Steps:   make([]Step, 0, len(payload.Steps)),
	}
	for _, step := range payload.Steps {
		profile.Steps = append(profile.Steps, Step{
			Sequence:         step.Sequence,
			Name:             step.Name,
			Type:             StepType(step.Type),
			Package:          step.Package,
			Path:             step.Path,
			Contains:         step.Contains,
			Service:          step.Service,
			Port:             step.Port,
			Command:          step.Command,
			ExpectedExitCode: step.ExpectedExitCode,
			TimeoutSeconds:   step.TimeoutSeconds,
		})
	}
	return profile
}

func (s *Service) saveFailed(ctx context.Context, cause contracts.Envelope, run RunRecord, code string, message string) error {
	run.State = runStateError
	run.ErrorCode = code
	run.ErrorMessage = message
	run.FinishedAt = time.Now().UTC()
	event, err := s.newEvent(cause, events.CheckerFailedV1, events.FailurePayload{
		LabRunID: run.LabRunID,
		Code:     code,
		Message:  message,
	})
	if err != nil {
		return err
	}
	event.Error = &contracts.MessageError{Code: code, Message: message}
	return s.repo.SaveFailed(ctx, cause, run, event)
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

func eventResults(results []StepResult) []events.CheckStepResult {
	converted := make([]events.CheckStepResult, 0, len(results))
	for _, result := range results {
		converted = append(converted, events.CheckStepResult{
			Name:    result.Name,
			Passed:  result.Passed,
			Message: result.Message,
		})
	}
	return converted
}

func allPassed(results []StepResult) bool {
	for _, result := range results {
		if !result.Passed {
			return false
		}
	}
	return len(results) > 0
}

func decodePayload(envelope contracts.Envelope, dst any) error {
	if err := json.Unmarshal(envelope.Payload, dst); err != nil {
		return fmt.Errorf("decode %s payload: %w", envelope.MessageType, err)
	}
	return nil
}
