package lifecycle

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/contracts/events"
)

func TestServiceSchedulesCleanupWithCommandTTL(t *testing.T) {
	repo := newFakeRepository()
	service := newTestService(t, repo)
	envelope := testEnvelope(t, commands.LifecycleScheduleCleanupV1, commands.LifecycleScheduleCleanupV1Payload{
		LabRunID:   testLabRunID,
		TTLSeconds: 30,
	})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if repo.timer.Kind != TimerKindCleanup || repo.timer.State != TimerStateScheduled {
		t.Fatalf("timer = %#v", repo.timer)
	}
	if !repo.timer.DueAt.Equal(testNow.Add(30 * time.Second)) {
		t.Fatalf("due_at = %v", repo.timer.DueAt)
	}
	if repo.event.MessageType != events.LifecycleCleanupScheduledV1.String() {
		t.Fatalf("event type = %s", repo.event.MessageType)
	}
}

func TestServiceSchedulesCleanupWithRuntimeDefault(t *testing.T) {
	repo := newFakeRepository()
	repo.settings.LabTTLSeconds = 45
	service := newTestService(t, repo)
	envelope := testEnvelope(t, commands.LifecycleScheduleCleanupV1, commands.LifecycleScheduleCleanupV1Payload{
		LabRunID: testLabRunID,
	})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !repo.timer.DueAt.Equal(testNow.Add(45 * time.Second)) {
		t.Fatalf("due_at = %v", repo.timer.DueAt)
	}
}

func TestServiceFreezesLabAndMovesCleanupTimer(t *testing.T) {
	repo := newFakeRepository()
	repo.settings.FreezeTTLSeconds = 60
	service := newTestService(t, repo)
	envelope := testEnvelope(t, commands.LifecycleFreezeLabV1, commands.LifecycleFreezeLabV1Payload{
		LabRunID: testLabRunID,
		Reason:   "support",
	})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !repo.timer.DueAt.Equal(testNow.Add(time.Minute)) || repo.timer.Reason != "support" {
		t.Fatalf("timer = %#v", repo.timer)
	}
	if repo.event.MessageType != events.LifecycleLabFrozenV1.String() {
		t.Fatalf("event type = %s", repo.event.MessageType)
	}
}

func TestServiceSavesRuntimeSettings(t *testing.T) {
	repo := newFakeRepository()
	service := newTestService(t, repo)
	envelope := testEnvelope(t, commands.SettingsUpdateV1, commands.SettingsUpdateV1Payload{
		ChangedBy: "teacher-1",
		Values: map[string]any{
			SettingLabTTLSeconds:     float64(120),
			SettingFreezeTTLSeconds:  float64(240),
			SettingCapacityThreshold: float64(85),
		},
	})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if repo.changedBy != "teacher-1" || len(repo.savedSettings) != 3 {
		t.Fatalf("settings = %q %#v", repo.changedBy, repo.savedSettings)
	}
	if repo.event.MessageType != events.SettingsChangedV1.String() {
		t.Fatalf("event type = %s", repo.event.MessageType)
	}
}

func TestServiceFiresDueTimers(t *testing.T) {
	repo := newFakeRepository()
	service := newTestService(t, repo)

	fired, err := service.FireDueTimers(context.Background(), 25)
	if err != nil {
		t.Fatalf("FireDueTimers: %v", err)
	}
	if fired != 2 {
		t.Fatalf("fired = %d", fired)
	}
	if !repo.fireNow.Equal(testNow) || repo.fireLimit != 25 || repo.fireProducer != "lifecycle-service" {
		t.Fatalf("fire args = %v/%d/%q", repo.fireNow, repo.fireLimit, repo.fireProducer)
	}
}

type fakeRepository struct {
	settings      RuntimeSettings
	timer         Timer
	event         contracts.Envelope
	savedSettings map[string]any
	changedBy     string
	fireNow       time.Time
	fireLimit     int
	fireProducer  string
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		settings: RuntimeSettings{
			LabTTLSeconds:            7200,
			FreezeTTLSeconds:         86400,
			CapacityThresholdPercent: 90,
		},
	}
}

func (r *fakeRepository) LoadSettings(context.Context, RuntimeSettings) (RuntimeSettings, error) {
	return r.settings, nil
}

func (r *fakeRepository) ScheduleCleanup(_ context.Context, _ contracts.Envelope, timer Timer, event contracts.Envelope) error {
	r.timer = timer
	r.event = event
	return nil
}

func (r *fakeRepository) FreezeLab(_ context.Context, _ contracts.Envelope, timer Timer, event contracts.Envelope) error {
	r.timer = timer
	r.event = event
	return nil
}

func (r *fakeRepository) CancelCleanup(context.Context, contracts.Envelope, string, string) error {
	return nil
}

func (r *fakeRepository) SaveSettings(_ context.Context, _ contracts.Envelope, changedBy string, values map[string]any, event contracts.Envelope) error {
	r.changedBy = changedBy
	r.savedSettings = values
	r.event = event
	return nil
}

func (r *fakeRepository) FireDueTimers(_ context.Context, now time.Time, limit int, producer string) (int, error) {
	r.fireNow = now
	r.fireLimit = limit
	r.fireProducer = producer
	return 2, nil
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func newTestService(t *testing.T, repo Repository) *Service {
	t.Helper()
	service, err := NewServiceWithClock("lifecycle-service", repo, config.LifecycleConfig{
		DefaultLabTTL:            2 * time.Hour,
		DefaultFreezeTTL:         24 * time.Hour,
		DefaultCapacityThreshold: 90,
	}, fixedClock{now: testNow})
	if err != nil {
		t.Fatalf("NewServiceWithClock: %v", err)
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
		AggregateID:    testLabRunID,
		IdempotencyKey: "test:" + subject.String(),
		Payload:        rawPayload,
	}
}

var (
	testNow      = time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	testLabRunID = "33333333-3333-4333-8333-333333333333"
)
