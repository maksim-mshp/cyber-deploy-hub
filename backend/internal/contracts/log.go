package contracts

import "log/slog"

func LogAttrs(envelope Envelope) []slog.Attr {
	return []slog.Attr{
		slog.String("message_id", envelope.MessageID),
		slog.String("message_type", envelope.MessageType),
		slog.String("message_kind", string(envelope.MessageKind)),
		slog.String("correlation_id", envelope.CorrelationID),
		slog.String("causation_id", envelope.CausationID),
		slog.String("saga_id", envelope.SagaID),
		slog.String("aggregate_type", envelope.AggregateType),
		slog.String("aggregate_id", envelope.AggregateID),
	}
}
