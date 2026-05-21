package lifecycle

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/inbox"
	"cyber-deploy-hub/internal/outbox"
	natsbus "cyber-deploy-hub/internal/transport/nats"
	lifecycleusecase "cyber-deploy-hub/internal/usecase/lifecycle"
)

const serviceName = "lifecycle-service"

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	dbConfig, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		return err
	}
	dbConfig.MaxConns = cfg.Database.MaxConns

	db, err := pgxpool.NewWithConfig(ctx, dbConfig)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		return err
	}

	bus, err := natsbus.NewPublisher(ctx, cfg.NATS.URL, serviceName, logger)
	if err != nil {
		return err
	}
	defer bus.Close()

	outboxStore, err := outbox.NewPostgresStore(db, "lifecycle")
	if err != nil {
		return err
	}
	inboxStore, err := inbox.NewPostgresStore(db, "lifecycle")
	if err != nil {
		return err
	}
	repo, err := lifecycleusecase.NewPostgresRepository(db)
	if err != nil {
		return err
	}
	service, err := lifecycleusecase.NewService(serviceName, repo, cfg.Lifecycle)
	if err != nil {
		return err
	}

	dispatcherCtx, stopDispatcher := context.WithCancel(ctx)
	defer stopDispatcher()
	dispatcher := outbox.NewDispatcher(outboxStore, bus, logger, outbox.DispatcherOptions{
		BatchSize: 25,
		Interval:  500 * time.Millisecond,
	})
	go dispatcher.Run(dispatcherCtx)

	scheduler := lifecycleusecase.NewScheduler(service, logger, cfg.Lifecycle.PollInterval, cfg.Lifecycle.DueBatchSize)
	go scheduler.Run(dispatcherCtx)

	consumers, err := startConsumers(ctx, cfg, inboxStore, service, logger)
	if err != nil {
		return err
	}
	defer func() {
		for _, consumer := range consumers {
			consumer.Close()
		}
	}()

	logger.Info("lifecycle service started",
		slog.Duration("poll_interval", nonZeroDuration(cfg.Lifecycle.PollInterval, 5*time.Second)),
		slog.Int("due_batch_size", nonZeroInt(cfg.Lifecycle.DueBatchSize, 25)),
	)
	<-ctx.Done()
	return ctx.Err()
}

func startConsumers(ctx context.Context, cfg config.Config, inboxStore *inbox.PostgresStore, service *lifecycleusecase.Service, logger *slog.Logger) ([]*natsbus.Consumer, error) {
	specs := []struct {
		name    string
		subject string
		durable string
	}{
		{name: "commands", subject: "cmd.lifecycle.>", durable: "lifecycle_commands"},
		{name: "settings", subject: "cmd.settings.update.v1", durable: "lifecycle_settings_commands"},
	}
	consumers := make([]*natsbus.Consumer, 0, len(specs))
	for _, spec := range specs {
		consumer, err := natsbus.NewConsumer(ctx, cfg.NATS.URL, serviceName+"-"+spec.name, natsbus.ConsumerOptions{
			Stream:  "COMMANDS",
			Subject: spec.subject,
			Queue:   serviceName,
			Durable: spec.durable,
		}, inboxStore, service.Handle, logger)
		if err != nil {
			for _, started := range consumers {
				started.Close()
			}
			return nil, err
		}
		if err := consumer.Start(); err != nil {
			consumer.Close()
			for _, started := range consumers {
				started.Close()
			}
			return nil, err
		}
		consumers = append(consumers, consumer)
	}
	return consumers, nil
}

func nonZeroDuration(value time.Duration, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func nonZeroInt(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
