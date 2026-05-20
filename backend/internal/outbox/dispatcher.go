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
	MarkFailed(ctx context.Context, id int64, err error) error
}

type Publisher interface {
	Publish(ctx context.Context, subject string, envelope contracts.Envelope) error
}

type DispatcherOptions struct {
	BatchSize int
	Interval  time.Duration
}

type Dispatcher struct {
	store     Store
	publisher Publisher
	logger    *slog.Logger
	batchSize int
	interval  time.Duration
}

func NewDispatcher(store Store, publisher Publisher, logger *slog.Logger, opts DispatcherOptions) *Dispatcher {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 10
	}
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}
	return &Dispatcher{
		store:     store,
		publisher: publisher,
		logger:    logger,
		batchSize: opts.BatchSize,
		interval:  opts.Interval,
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
		d.logError("failed to fetch outbox messages", err)
		return
	}

	for _, message := range messages {
		if err := d.publisher.Publish(ctx, message.Subject, message.Envelope); err != nil {
			d.logError("failed to publish outbox message", err)
			if markErr := d.store.MarkFailed(ctx, message.ID, err); markErr != nil {
				d.logError("failed to mark outbox message as failed", markErr)
			}
			continue
		}

		if err := d.store.MarkPublished(ctx, message.ID); err != nil {
			d.logError("failed to mark outbox message as published", err)
		}
	}
}

func (d *Dispatcher) logError(message string, err error) {
	if d.logger != nil {
		d.logger.Error(message, slog.Any("error", err))
	}
}
