package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"cyber-deploy-hub/internal/contracts"
)

func TestDispatcherPublishesAndMarksMessage(t *testing.T) {
	store := &fakeStore{
		messages: []Message{testMessage(1, 1)},
	}
	publisher := &fakePublisher{}
	dispatcher := NewDispatcher(store, publisher, nil, DispatcherOptions{BatchSize: 10})

	dispatcher.publishBatch(context.Background())

	if len(publisher.subjects) != 1 || publisher.subjects[0] != "cmd.test.run.v1" {
		t.Fatalf("published subjects = %#v", publisher.subjects)
	}
	if len(store.publishedIDs) != 1 || store.publishedIDs[0] != 1 {
		t.Fatalf("published ids = %#v", store.publishedIDs)
	}
}

func TestDispatcherRetriesWithBackoff(t *testing.T) {
	store := &fakeStore{
		messages: []Message{testMessage(1, 2)},
	}
	publisher := &fakePublisher{failSubjects: map[string]error{"cmd.test.run.v1": errors.New("nats down")}}
	dispatcher := NewDispatcher(store, publisher, nil, DispatcherOptions{
		MaxAttempts:    5,
		InitialBackoff: time.Second,
		MaxBackoff:     10 * time.Second,
	})

	dispatcher.publishBatch(context.Background())

	if len(store.failed) != 1 {
		t.Fatalf("failed marks = %#v", store.failed)
	}
	if store.failed[0].delay != 2*time.Second {
		t.Fatalf("retry delay = %v, want 2s", store.failed[0].delay)
	}
	if len(store.deadLetters) != 0 {
		t.Fatalf("dead letters = %#v", store.deadLetters)
	}
}

func TestDispatcherDeadLettersAfterMaxAttempts(t *testing.T) {
	store := &fakeStore{
		messages: []Message{testMessage(7, 3)},
	}
	publisher := &fakePublisher{failSubjects: map[string]error{"cmd.test.run.v1": errors.New("nats down")}}
	dispatcher := NewDispatcher(store, publisher, nil, DispatcherOptions{
		MaxAttempts:    3,
		InitialBackoff: time.Second,
		DLQPrefix:      "dlq.",
	})

	dispatcher.publishBatch(context.Background())

	if len(store.deadLetters) != 1 || store.deadLetters[0] != 7 {
		t.Fatalf("dead letters = %#v", store.deadLetters)
	}
	if len(store.failed) != 0 {
		t.Fatalf("failed marks = %#v", store.failed)
	}
	if len(publisher.subjects) != 2 || publisher.subjects[1] != "dlq.cmd.test.run.v1" {
		t.Fatalf("published subjects = %#v", publisher.subjects)
	}
}

func TestBackoffDelayCapsAtMax(t *testing.T) {
	got := BackoffDelay(10, time.Second, 5*time.Second)
	if got != 5*time.Second {
		t.Fatalf("BackoffDelay = %v, want 5s", got)
	}
}

func testMessage(id int64, attempts int) Message {
	envelope, err := contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:          contracts.MessageKindCommand,
		Type:          contracts.Subject("cmd.test.run.v1"),
		Producer:      "test",
		SagaID:        "saga-1",
		AggregateType: "lab_run",
		AggregateID:   "lab-1",
		Payload:       map[string]string{"ok": "true"},
	})
	if err != nil {
		panic(err)
	}
	return Message{
		ID:       id,
		Subject:  "cmd.test.run.v1",
		Attempts: attempts,
		Envelope: envelope,
	}
}

type fakeStore struct {
	messages     []Message
	publishedIDs []int64
	failed       []failedMark
	deadLetters  []int64
}

type failedMark struct {
	id    int64
	delay time.Duration
}

func (s *fakeStore) FetchPending(context.Context, int) ([]Message, error) {
	return s.messages, nil
}

func (s *fakeStore) MarkPublished(_ context.Context, id int64) error {
	s.publishedIDs = append(s.publishedIDs, id)
	return nil
}

func (s *fakeStore) MarkFailed(_ context.Context, id int64, _ error, delay time.Duration) error {
	s.failed = append(s.failed, failedMark{id: id, delay: delay})
	return nil
}

func (s *fakeStore) MarkDeadLetter(_ context.Context, id int64, _ error) error {
	s.deadLetters = append(s.deadLetters, id)
	return nil
}

type fakePublisher struct {
	subjects     []string
	failSubjects map[string]error
}

func (p *fakePublisher) Publish(_ context.Context, subject string, _ contracts.Envelope) error {
	p.subjects = append(p.subjects, subject)
	if err := p.failSubjects[subject]; err != nil {
		return err
	}
	return nil
}
