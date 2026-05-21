package settings

import (
	"context"
	"testing"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
)

func TestServiceUpdatePublishesSettingsCommand(t *testing.T) {
	outbox := &fakeOutbox{}
	service := NewService("api-gateway-service", nil, outbox)

	result, err := service.Update(context.Background(), UpdateRequest{
		ChangedBy: "teacher-1",
		Values: map[string]any{
			"lab_ttl_seconds": float64(7200),
		},
		IdempotencyKey: "settings:test",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if result.Status != "ACCEPTED" || result.CommandID == "" {
		t.Fatalf("result = %#v", result)
	}
	if outbox.envelope.MessageType != commands.SettingsUpdateV1.String() {
		t.Fatalf("message type = %s", outbox.envelope.MessageType)
	}
	if outbox.envelope.AggregateType != "settings" || outbox.envelope.AggregateID != "runtime" {
		t.Fatalf("aggregate = %s/%s", outbox.envelope.AggregateType, outbox.envelope.AggregateID)
	}
}

type fakeOutbox struct {
	envelope contracts.Envelope
}

func (o *fakeOutbox) Enqueue(_ context.Context, envelope contracts.Envelope) error {
	o.envelope = envelope
	return nil
}
