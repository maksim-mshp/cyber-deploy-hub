package contracts

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type MessageKind string

const (
	MessageKindCommand MessageKind = "COMMAND"
	MessageKindEvent   MessageKind = "EVENT"
)

type Envelope struct {
	MessageID      string          `json:"message_id"`
	MessageKind    MessageKind     `json:"message_kind"`
	MessageType    string          `json:"message_type"`
	SchemaVersion  int             `json:"schema_version"`
	OccurredAt     time.Time       `json:"occurred_at"`
	Producer       string          `json:"producer"`
	CorrelationID  string          `json:"correlation_id"`
	CausationID    string          `json:"causation_id,omitempty"`
	SagaID         string          `json:"saga_id"`
	AggregateType  string          `json:"aggregate_type"`
	AggregateID    string          `json:"aggregate_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload"`
	Error          *MessageError   `json:"error"`
}

type MessageError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type NewEnvelopeParams struct {
	Kind           MessageKind
	Type           Subject
	Producer       string
	CorrelationID  string
	CausationID    string
	SagaID         string
	AggregateType  string
	AggregateID    string
	IdempotencyKey string
	Payload        any
}

func NewEnvelope(params NewEnvelopeParams) (Envelope, error) {
	payload, err := json.Marshal(params.Payload)
	if err != nil {
		return Envelope{}, err
	}

	messageID := uuid.NewString()
	correlationID := params.CorrelationID
	if correlationID == "" {
		correlationID = messageID
	}
	idempotencyKey := params.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}

	return Envelope{
		MessageID:      messageID,
		MessageKind:    params.Kind,
		MessageType:    params.Type.String(),
		SchemaVersion:  1,
		OccurredAt:     time.Now().UTC(),
		Producer:       params.Producer,
		CorrelationID:  correlationID,
		CausationID:    params.CausationID,
		SagaID:         params.SagaID,
		AggregateType:  params.AggregateType,
		AggregateID:    params.AggregateID,
		IdempotencyKey: idempotencyKey,
		Payload:        payload,
		Error:          nil,
	}, nil
}
