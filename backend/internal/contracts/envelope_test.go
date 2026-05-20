package contracts

import (
	"encoding/json"
	"testing"
)

func TestNewEnvelopeUsesTypedSubjectAndDefaults(t *testing.T) {
	envelope, err := NewEnvelope(NewEnvelopeParams{
		Kind:          MessageKindCommand,
		Type:          Subject("cmd.test.run.v1"),
		Producer:      "test-service",
		SagaID:        "saga-1",
		AggregateType: "lab_run",
		AggregateID:   "lab-1",
		Payload: map[string]string{
			"key": "value",
		},
	})
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}

	if envelope.MessageType != "cmd.test.run.v1" {
		t.Fatalf("MessageType = %q", envelope.MessageType)
	}
	if envelope.MessageID == "" {
		t.Fatal("MessageID must be generated")
	}
	if envelope.CorrelationID != envelope.MessageID {
		t.Fatalf("CorrelationID = %q, want message id", envelope.CorrelationID)
	}
	if envelope.IdempotencyKey == "" {
		t.Fatal("IdempotencyKey must be generated")
	}

	var payload map[string]string
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["key"] != "value" {
		t.Fatalf("payload key = %q", payload["key"])
	}
}

func TestSubjectDLQ(t *testing.T) {
	if got := Subject("cmd.lab.request_provision.v1").DLQ(); got != "dlq.cmd.lab.request_provision.v1" {
		t.Fatalf("DLQ subject = %q", got)
	}
	if got := Subject("dlq.cmd.lab.request_provision.v1").DLQ(); got != "dlq.cmd.lab.request_provision.v1" {
		t.Fatalf("DLQ subject must be idempotent, got %q", got)
	}
}
