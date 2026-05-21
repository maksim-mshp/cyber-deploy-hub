package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/contracts/events"
)

const aggregateTypeLabRun = "lab_run"

type Repository interface {
	LoadSettings(ctx context.Context, defaults RuntimeSettings) (RuntimeSettings, error)
	ScheduleCleanup(ctx context.Context, command contracts.Envelope, timer Timer, event contracts.Envelope) error
	FreezeLab(ctx context.Context, command contracts.Envelope, timer Timer, event contracts.Envelope) error
	CancelCleanup(ctx context.Context, command contracts.Envelope, labRunID string, reason string) error
	SaveSettings(ctx context.Context, command contracts.Envelope, changedBy string, values map[string]any, event contracts.Envelope) error
	FireDueTimers(ctx context.Context, now time.Time, limit int, producer string) (int, error)
}

type Service struct {
	producer string
	repo     Repository
	clock    clock
	defaults RuntimeSettings
}

func NewService(producer string, repo Repository, cfg config.LifecycleConfig) (*Service, error) {
	return NewServiceWithClock(producer, repo, cfg, systemClock{})
}

func NewServiceWithClock(producer string, repo Repository, cfg config.LifecycleConfig, clk clock) (*Service, error) {
	if strings.TrimSpace(producer) == "" {
		return nil, errors.New("producer is empty")
	}
	if repo == nil {
		return nil, errors.New("repository is nil")
	}
	if clk == nil {
		return nil, errors.New("clock is nil")
	}
	return &Service{
		producer: producer,
		repo:     repo,
		clock:    clk,
		defaults: DefaultSettings(cfg),
	}, nil
}

func (s *Service) Handle(ctx context.Context, envelope contracts.Envelope) error {
	switch envelope.MessageType {
	case commands.LifecycleScheduleCleanupV1.String():
		return s.handleScheduleCleanup(ctx, envelope)
	case commands.LifecycleFreezeLabV1.String():
		return s.handleFreezeLab(ctx, envelope)
	case commands.LifecycleCancelTimerV1.String():
		return s.handleCancelTimer(ctx, envelope)
	case commands.SettingsUpdateV1.String():
		return s.handleSettingsUpdate(ctx, envelope)
	default:
		return nil
	}
}

func (s *Service) FireDueTimers(ctx context.Context, limit int) (int, error) {
	return s.repo.FireDueTimers(ctx, s.clock.Now(), limit, s.producer)
}

func (s *Service) handleScheduleCleanup(ctx context.Context, envelope contracts.Envelope) error {
	var payload commands.LifecycleScheduleCleanupV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	if strings.TrimSpace(payload.LabRunID) == "" {
		return errors.New("lab_run_id is required")
	}
	settings, err := s.repo.LoadSettings(ctx, s.defaults)
	if err != nil {
		return err
	}
	ttlSeconds := payload.TTLSeconds
	if ttlSeconds <= 0 {
		ttlSeconds = settings.LabTTLSeconds
	}
	timer := Timer{
		LabRunID:           payload.LabRunID,
		Kind:               TimerKindCleanup,
		State:              TimerStateScheduled,
		DueAt:              s.clock.Now().Add(time.Duration(ttlSeconds) * time.Second),
		Reason:             "scheduled",
		CreatedByMessageID: envelope.MessageID,
	}
	event, err := s.newEvent(envelope, events.LifecycleCleanupScheduledV1, events.LifecycleCleanupScheduledV1Payload{
		LabRunID: payload.LabRunID,
		DueAt:    timer.DueAt.Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	return s.repo.ScheduleCleanup(ctx, envelope, timer, event)
}

func (s *Service) handleFreezeLab(ctx context.Context, envelope contracts.Envelope) error {
	var payload commands.LifecycleFreezeLabV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	if strings.TrimSpace(payload.LabRunID) == "" {
		return errors.New("lab_run_id is required")
	}
	settings, err := s.repo.LoadSettings(ctx, s.defaults)
	if err != nil {
		return err
	}
	freezeSeconds := payload.FreezeSeconds
	if freezeSeconds <= 0 {
		freezeSeconds = settings.FreezeTTLSeconds
	}
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = "support_freeze"
	}
	timer := Timer{
		LabRunID:           payload.LabRunID,
		Kind:               TimerKindCleanup,
		State:              TimerStateScheduled,
		DueAt:              s.clock.Now().Add(time.Duration(freezeSeconds) * time.Second),
		Reason:             reason,
		CreatedByMessageID: envelope.MessageID,
	}
	event, err := s.newEvent(envelope, events.LifecycleLabFrozenV1, events.LabRunEventPayload{
		LabRunID: payload.LabRunID,
		State:    "FROZEN",
	})
	if err != nil {
		return err
	}
	return s.repo.FreezeLab(ctx, envelope, timer, event)
}

func (s *Service) handleCancelTimer(ctx context.Context, envelope contracts.Envelope) error {
	var payload commands.LifecycleCancelTimerV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	if strings.TrimSpace(payload.LabRunID) == "" {
		return errors.New("lab_run_id is required")
	}
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = "cancelled"
	}
	return s.repo.CancelCleanup(ctx, envelope, payload.LabRunID, reason)
}

func (s *Service) handleSettingsUpdate(ctx context.Context, envelope contracts.Envelope) error {
	var payload commands.SettingsUpdateV1Payload
	if err := decodePayload(envelope, &payload); err != nil {
		return err
	}
	if len(payload.Values) == 0 {
		return errors.New("settings values are required")
	}
	if _, err := MergeSettings(s.defaults, payload.Values); err != nil {
		return err
	}
	event, err := s.newEvent(envelope, events.SettingsChangedV1, events.SettingsChangedV1Payload{
		ChangedBy: payload.ChangedBy,
		Values:    payload.Values,
	})
	if err != nil {
		return err
	}
	return s.repo.SaveSettings(ctx, envelope, payload.ChangedBy, payload.Values, event)
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

func decodePayload(envelope contracts.Envelope, dst any) error {
	if err := json.Unmarshal(envelope.Payload, dst); err != nil {
		return fmt.Errorf("decode %s payload: %w", envelope.MessageType, err)
	}
	return nil
}
