package outbox

import (
	"context"
	"log/slog"
	"time"

	"cyber-deploy-hub/internal/contracts"
)

type Store interface {
	FetchPending(ctx context.Context, limit int) ([]Message, error)
	MarkPublished(ctx context.Context, id int64) error
	MarkFailed(ctx context.Context, id int64, err error, delay time.Duration) error
	MarkDeadLetter(ctx context.Context, id int64, err error) error
}

type Publisher interface {
	Publish(ctx context.Context, subject string, envelope contracts.Envelope) error
}

type DispatcherOptions struct {
	BatchSize      int
	Interval       time.Duration
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	DLQPrefix      string
}

type Dispatcher struct {
	store          Store
	publisher      Publisher
	logger         *slog.Logger
	batchSize      int
	interval       time.Duration
	maxAttempts    int
	initialBackoff time.Duration
	maxBackoff     time.Duration
	dlqPrefix      string
}

func NewDispatcher(store Store, publisher Publisher, logger *slog.Logger, opts DispatcherOptions) *Dispatcher {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 10
	}
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 5
	}
	if opts.InitialBackoff <= 0 {
		opts.InitialBackoff = 5 * time.Second
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = time.Minute
	}
	if opts.DLQPrefix == "" {
		opts.DLQPrefix = "dlq."
	}
	return &Dispatcher{
		store:          store,
		publisher:      publisher,
		logger:         logger,
		batchSize:      opts.BatchSize,
		interval:       opts.Interval,
		maxAttempts:    opts.MaxAttempts,
		initialBackoff: opts.InitialBackoff,
		maxBackoff:     opts.MaxBackoff,
		dlqPrefix:      opts.DLQPrefix,
	}
}

func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		d.publishBatch(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) publishBatch(ctx context.Context) {
	messages, err := d.store.FetchPending(ctx, d.batchSize)
	if err != nil {
		if d.logger != nil {
			d.logger.Error("failed to fetch outbox messages", slog.Any("error", err))
		}
		return
	}

	for _, message := range messages {
		if err := d.publisher.Publish(ctx, message.Subject, message.Envelope); err != nil {
			d.logError("failed to publish outbox message", err, message.Envelope)
			if message.Attempts >= d.maxAttempts {
				d.publishDLQ(ctx, message, err)
				if markErr := d.store.MarkDeadLetter(ctx, message.ID, err); markErr != nil {
					d.logError("failed to mark outbox message as dead letter", markErr, message.Envelope)
				}
				continue
			}

			delay := BackoffDelay(message.Attempts, d.initialBackoff, d.maxBackoff)
			if markErr := d.store.MarkFailed(ctx, message.ID, err, delay); markErr != nil {
				d.logError("failed to mark outbox message as failed", markErr, message.Envelope)
			}
			continue
		}

		if err := d.store.MarkPublished(ctx, message.ID); err != nil {
			d.logError("failed to mark outbox message as published", err, message.Envelope)
		}
	}
}

func (d *Dispatcher) publishDLQ(ctx context.Context, message Message, publishErr error) {
	envelope := message.Envelope
	envelope.Error = &contracts.MessageError{
		Code:    "OUTBOX_PUBLISH_FAILED",
		Message: truncateError(publishErr),
	}
	dlqSubject := d.dlqPrefix + message.Subject
	if err := d.publisher.Publish(ctx, dlqSubject, envelope); err != nil {
		d.logError("failed to publish outbox message to dlq", err, message.Envelope)
	}
}

func BackoffDelay(attempts int, initial time.Duration, max time.Duration) time.Duration {
	if attempts <= 1 {
		return initial
	}
	delay := initial
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= max {
			return max
		}
	}
	return delay
}

func (d *Dispatcher) logError(message string, err error, envelope contracts.Envelope) {
	if d.logger != nil {
		attrs := append(contracts.LogAttrs(envelope), slog.Any("error", err))
		d.logger.LogAttrs(context.Background(), slog.LevelError, message, attrs...)
	}
}
