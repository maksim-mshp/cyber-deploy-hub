package core

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/inbox"
	"cyber-deploy-hub/internal/outbox"
	natsbus "cyber-deploy-hub/internal/transport/nats"
	coreusecase "cyber-deploy-hub/internal/usecase/core"
)

const serviceName = "core-service"

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

	outboxStore, err := outbox.NewPostgresStore(db, "core")
	if err != nil {
		return err
	}
	inboxStore, err := inbox.NewPostgresStore(db, "core")
	if err != nil {
		return err
	}
	repo, err := coreusecase.NewPostgresRepository(db)
	if err != nil {
		return err
	}
	service := coreusecase.NewService(serviceName, repo)

	dispatcherCtx, stopDispatcher := context.WithCancel(ctx)
	defer stopDispatcher()
	dispatcher := outbox.NewDispatcher(outboxStore, bus, logger, outbox.DispatcherOptions{
		BatchSize: 25,
		Interval:  500 * time.Millisecond,
	})
	go dispatcher.Run(dispatcherCtx)

	consumers, err := startConsumers(ctx, cfg, inboxStore, service, logger)
	if err != nil {
		return err
	}
	defer func() {
		for _, consumer := range consumers {
			consumer.Close()
		}
	}()

	logger.Info("core service started")
	<-ctx.Done()
	return ctx.Err()
}

func startConsumers(
	ctx context.Context,
	cfg config.Config,
	inboxStore *inbox.PostgresStore,
	service *coreusecase.Service,
	logger *slog.Logger,
) ([]*natsbus.Consumer, error) {
	consumerConfigs := []struct {
		name    string
		options natsbus.ConsumerOptions
	}{
		{
			name: serviceName + "-commands",
			options: natsbus.ConsumerOptions{
				Stream:  "COMMANDS",
				Subject: "cmd.lab.>",
				Queue:   serviceName,
				Durable: "core_lab_commands",
			},
		},
		{
			name: serviceName + "-events",
			options: natsbus.ConsumerOptions{
				Stream:  "EVENTS",
				Subject: "evt.>",
				Queue:   serviceName,
				Durable: "core_events",
			},
		},
	}

	consumers := make([]*natsbus.Consumer, 0, len(consumerConfigs))
	for _, consumerConfig := range consumerConfigs {
		consumer, err := natsbus.NewConsumer(
			ctx,
			cfg.NATS.URL,
			consumerConfig.name,
			consumerConfig.options,
			inboxStore,
			service.Handle,
			logger,
		)
		if err != nil {
			closeConsumers(consumers)
			return nil, err
		}
		if err := consumer.Start(); err != nil {
			consumer.Close()
			closeConsumers(consumers)
			return nil, err
		}
		consumers = append(consumers, consumer)
	}
	return consumers, nil
}

func closeConsumers(consumers []*natsbus.Consumer) {
	for _, consumer := range consumers {
		consumer.Close()
	}
}
