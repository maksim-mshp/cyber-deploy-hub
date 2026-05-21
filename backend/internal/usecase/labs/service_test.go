package labs

import (
	"context"
	"testing"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
)

func TestServicePublishesFreezeCleanupAndCheckCommands(t *testing.T) {
	tests := []struct {
		name    string
		call    func(*Service, context.Context) (CommandAccepted, error)
		subject contracts.Subject
	}{
		{
			name: "freeze",
			call: func(service *Service, ctx context.Context) (CommandAccepted, error) {
				return service.RequestFreeze(ctx, LabCommand{LabRunID: testLabRunID, Reason: "support"})
			},
			subject: commands.RequestFreezeV1,
		},
		{
			name: "cleanup",
			call: func(service *Service, ctx context.Context) (CommandAccepted, error) {
				return service.RequestCleanup(ctx, LabCommand{LabRunID: testLabRunID, Reason: "manual"})
			},
			subject: commands.RequestCleanupV1,
		},
		{
			name: "check",
			call: func(service *Service, ctx context.Context) (CommandAccepted, error) {
				return service.RequestCheck(ctx, CheckCommand{LabRunID: testLabRunID, ProfileID: "default"})
			},
			subject: commands.RequestVerificationV1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outbox := &fakeOutbox{}
			service := NewService("api-gateway-service", nil, outbox)
			result, err := tt.call(service, context.Background())
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if result.Status != "ACCEPTED" || result.CommandID == "" {
				t.Fatalf("result = %#v", result)
			}
			if outbox.envelope.MessageType != tt.subject.String() {
				t.Fatalf("message type = %s, want %s", outbox.envelope.MessageType, tt.subject.String())
			}
			if outbox.envelope.AggregateID != testLabRunID {
				t.Fatalf("aggregate id = %s", outbox.envelope.AggregateID)
			}
		})
	}
}

type fakeOutbox struct {
	envelope contracts.Envelope
}

func (o *fakeOutbox) Enqueue(_ context.Context, envelope contracts.Envelope) error {
	o.envelope = envelope
	return nil
}

const testLabRunID = "33333333-3333-4333-8333-333333333333"
