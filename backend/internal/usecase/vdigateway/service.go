package vdigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/contracts/events"
)

const aggregateTypeLabRun = "lab_run"

type Repository interface {
	SaveIssued(ctx context.Context, command contracts.Envelope, token AccessToken, event contracts.Envelope) error
	SaveIssueFailure(ctx context.Context, command contracts.Envelope, labRunID string, studentID string, projectID string, reason string, event contracts.Envelope) error
	RevokeByLabRun(ctx context.Context, command contracts.Envelope, labRunID string, reason string, event contracts.Envelope) error
	FindToken(ctx context.Context, tokenHash string) (AccessToken, bool, error)
	FindInstanceTarget(ctx context.Context, labRunID string, serverID string) (InstanceTarget, bool, error)
	FindDefaultInstanceTarget(ctx context.Context, labRunID string) (InstanceTarget, bool, error)
	MarkExpired(ctx context.Context, tokenHash string) error
	RecordOpen(ctx context.Context, tokenHash string, labRunID string, openedAt time.Time, remoteAddr string, userAgent string) error
}

type ConsoleProvider interface {
	ConsoleURL(ctx context.Context, serverID string) (string, error)
}

type Service struct {
	producer        string
	repo            Repository
	consoleProvider ConsoleProvider
	tokenGenerator  TokenGenerator
	clock           clock
	accessTokenTTL  time.Duration
	publicBaseURL   string
}

func NewService(producer string, repo Repository, cfg config.VDIConfig, consoleProvider ConsoleProvider) (*Service, error) {
	return NewServiceWithDeps(producer, repo, cfg, consoleProvider, SecureTokenGenerator{}, systemClock{})
}

func NewServiceWithDeps(producer string, repo Repository, cfg config.VDIConfig, consoleProvider ConsoleProvider, generator TokenGenerator, clk clock) (*Service, error) {
	if strings.TrimSpace(producer) == "" {
		return nil, errors.New("producer is empty")
	}
	if repo == nil {
		return nil, errors.New("repository is nil")
	}
	if consoleProvider == nil {
		return nil, errors.New("console provider is nil")
	}
	if generator == nil {
		return nil, errors.New("token generator is nil")
	}
	if clk == nil {
		return nil, errors.New("clock is nil")
	}
	ttl := cfg.AccessTokenTTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &Service{
		producer:        producer,
		repo:            repo,
		consoleProvider: consoleProvider,
		tokenGenerator:  generator,
		clock:           clk,
		accessTokenTTL:  ttl,
		publicBaseURL:   strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"),
	}, nil
}

func (s *Service) Handle(ctx context.Context, envelope contracts.Envelope) error {
	switch envelope.MessageType {
	case commands.VDIIssueAccessV1.String():
		return s.handleIssue(ctx, envelope)
	case commands.VDIRevokeAccessV1.String():
		return s.handleRevokeCommand(ctx, envelope)
	case events.LifecycleLabFrozenV1.String():
		return s.handleRevokeEvent(ctx, envelope, "lab_frozen")
	case events.CloudLabCleanedV1.String():
		return s.handleRevokeEvent(ctx, envelope, "cloud_lab_cleaned")
	case events.LabFailedV1.String():
		return s.handleRevokeEvent(ctx, envelope, "lab_failed")
	default:
		return nil
	}
}

func (s *Service) IssueAccess(ctx context.Context, envelope contracts.Envelope) (IssueResult, error) {
	var payload commands.VDIIssueAccessV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return IssueResult{}, err
	}
	if err := validateIssuePayload(payload); err != nil {
		event, eventErr := s.failureEvent(envelope, payload.LabRunID, "VDI_ACCESS_ISSUE_REJECTED", err.Error())
		if eventErr != nil {
			return IssueResult{}, eventErr
		}
		return IssueResult{}, s.repo.SaveIssueFailure(ctx, envelope, payload.LabRunID, payload.StudentID, payload.ProjectID, err.Error(), event)
	}

	rawToken, err := s.tokenGenerator.Generate()
	if err != nil {
		event, eventErr := s.failureEvent(envelope, payload.LabRunID, "VDI_TOKEN_GENERATION_FAILED", err.Error())
		if eventErr != nil {
			return IssueResult{}, eventErr
		}
		return IssueResult{}, s.repo.SaveIssueFailure(ctx, envelope, payload.LabRunID, payload.StudentID, payload.ProjectID, err.Error(), event)
	}
	hash, err := TokenHash(rawToken)
	if err != nil {
		return IssueResult{}, err
	}

	now := s.clock.Now()
	token := AccessToken{
		TokenHash: hash,
		LabRunID:  payload.LabRunID,
		StudentID: payload.StudentID,
		ProjectID: payload.ProjectID,
		State:     TokenStateActive,
		ExpiresAt: now.Add(s.accessTokenTTL),
		IssuedAt:  now,
	}
	accessURL := publicSessionURL(s.publicBaseURL, rawToken)
	event, err := s.newEvent(envelope, events.VDIAccessIssuedV1, events.VDIAccessIssuedV1Payload{
		LabRunID:  payload.LabRunID,
		StudentID: payload.StudentID,
		URL:       accessURL,
		ExpiresAt: token.ExpiresAt.Format(time.RFC3339),
	})
	if err != nil {
		return IssueResult{}, err
	}
	if err := s.repo.SaveIssued(ctx, envelope, token, event); err != nil {
		return IssueResult{}, err
	}
	return IssueResult{AccessURL: accessURL, ExpiresAt: token.ExpiresAt}, nil
}

func (s *Service) OpenSession(ctx context.Context, req OpenSessionRequest) (SessionLaunch, error) {
	hash, err := TokenHash(req.Token)
	if err != nil {
		return SessionLaunch{}, err
	}
	token, found, err := s.repo.FindToken(ctx, hash)
	if err != nil {
		return SessionLaunch{}, err
	}
	if !found {
		return SessionLaunch{}, ErrTokenNotFound
	}
	switch token.State {
	case TokenStateRevoked:
		return SessionLaunch{}, ErrTokenRevoked
	case TokenStateExpired:
		return SessionLaunch{}, ErrTokenExpired
	case TokenStateActive:
	default:
		return SessionLaunch{}, fmt.Errorf("unsupported vdi token state %q", token.State)
	}
	now := s.clock.Now()
	if !token.ExpiresAt.After(now) {
		if err := s.repo.MarkExpired(ctx, hash); err != nil {
			return SessionLaunch{}, err
		}
		return SessionLaunch{}, ErrTokenExpired
	}
	target, err := s.resolveTarget(ctx, token.LabRunID, req)
	if err != nil {
		return SessionLaunch{}, err
	}
	consoleURL, err := s.consoleProvider.ConsoleURL(ctx, target.ServerID)
	if err != nil {
		return SessionLaunch{}, fmt.Errorf("open vdi console for server %s: %w", target.ServerID, err)
	}
	if err := s.repo.RecordOpen(ctx, hash, token.LabRunID, now, req.RemoteAddr, req.UserAgent); err != nil {
		return SessionLaunch{}, err
	}
	return SessionLaunch{
		LaunchURL:    consoleURL,
		ExpiresAt:    token.ExpiresAt,
		ServerID:     target.ServerID,
		InstanceName: target.Name,
	}, nil
}

func (s *Service) handleIssue(ctx context.Context, envelope contracts.Envelope) error {
	_, err := s.IssueAccess(ctx, envelope)
	return err
}

func (s *Service) handleRevokeCommand(ctx context.Context, envelope contracts.Envelope) error {
	var payload commands.VDIRevokeAccessV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	if strings.TrimSpace(payload.LabRunID) == "" {
		return errors.New("lab_run_id is required")
	}
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = "requested"
	}
	return s.revoke(ctx, envelope, payload.LabRunID, reason)
}

func (s *Service) handleRevokeEvent(ctx context.Context, envelope contracts.Envelope, fallbackReason string) error {
	labRunID, err := labRunIDFromPayload(envelope)
	if err != nil {
		return err
	}
	if strings.TrimSpace(labRunID) == "" {
		return fmt.Errorf("event %s does not include lab_run_id", envelope.MessageType)
	}
	return s.revoke(ctx, envelope, labRunID, fallbackReason)
}

func (s *Service) revoke(ctx context.Context, envelope contracts.Envelope, labRunID string, reason string) error {
	event, err := s.newEvent(envelope, events.VDIAccessRevokedV1, events.VDIAccessRevokedV1Payload{
		LabRunID: labRunID,
		Reason:   reason,
	})
	if err != nil {
		return err
	}
	return s.repo.RevokeByLabRun(ctx, envelope, labRunID, reason, event)
}

func (s *Service) failureEvent(cause contracts.Envelope, labRunID string, code string, message string) (contracts.Envelope, error) {
	event, err := s.newEvent(cause, events.VDIAccessFailedV1, events.FailurePayload{
		LabRunID: labRunID,
		Code:     code,
		Message:  message,
	})
	if err != nil {
		return contracts.Envelope{}, err
	}
	event.Error = &contracts.MessageError{Code: code, Message: message}
	return event, nil
}

func (s *Service) newEvent(cause contracts.Envelope, subject contracts.Subject, payload any) (contracts.Envelope, error) {
	return contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:           contracts.MessageKindEvent,
		Type:           subject,
		Producer:       s.producer,
		CorrelationID:  cause.CorrelationID,
		CausationID:    cause.MessageID,
		SagaID:         cause.SagaID,
		AggregateType:  aggregateTypeLabRun,
		AggregateID:    cause.AggregateID,
		IdempotencyKey: cause.SagaID + ":" + subject.String(),
		Payload:        payload,
	})
}

func (s *Service) resolveTarget(ctx context.Context, labRunID string, req OpenSessionRequest) (InstanceTarget, error) {
	serverID := strings.TrimSpace(req.ServerID)
	if serverID == "" {
		target, found, err := s.repo.FindDefaultInstanceTarget(ctx, labRunID)
		if err != nil {
			return InstanceTarget{}, err
		}
		if !found || target.State != "ACTIVE" || target.ServerID == "" {
			return InstanceTarget{}, ErrInstanceNotFound
		}
		return target, nil
	}
	target, found, err := s.repo.FindInstanceTarget(ctx, labRunID, serverID)
	if err != nil {
		return InstanceTarget{}, err
	}
	if !found || target.State != "ACTIVE" {
		return InstanceTarget{}, ErrInstanceNotFound
	}
	if target.Name == "" {
		target.Name = strings.TrimSpace(req.InstanceName)
	}
	return target, nil
}

func validateIssuePayload(payload commands.VDIIssueAccessV1Payload) error {
	if strings.TrimSpace(payload.LabRunID) == "" {
		return errors.New("lab_run_id is required")
	}
	if strings.TrimSpace(payload.StudentID) == "" {
		return errors.New("student_id is required")
	}
	if strings.TrimSpace(payload.ProjectID) == "" {
		return errors.New("project_id is required")
	}
	return nil
}

func publicSessionURL(baseURL string, token string) string {
	path := "/vdi/session/" + url.PathEscape(token)
	if strings.TrimSpace(baseURL) == "" {
		return path
	}
	return strings.TrimRight(baseURL, "/") + path
}

func decodePayload(envelope contracts.Envelope, dst any) error {
	if err := json.Unmarshal(envelope.Payload, dst); err != nil {
		return fmt.Errorf("decode %s payload: %w", envelope.MessageType, err)
	}
	return nil
}

func labRunIDFromPayload(envelope contracts.Envelope) (string, error) {
	var payload struct {
		LabRunID string `json:"lab_run_id"`
	}
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return "", fmt.Errorf("decode %s lab_run_id: %w", envelope.MessageType, err)
	}
	return payload.LabRunID, nil
}
