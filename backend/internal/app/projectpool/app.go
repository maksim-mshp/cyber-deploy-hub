package projectpool

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/inbox"
	"cyber-deploy-hub/internal/outbox"
	natsbus "cyber-deploy-hub/internal/transport/nats"
	projectpoolusecase "cyber-deploy-hub/internal/usecase/projectpool"
)

const serviceName = "project-pool-service"

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

	outboxStore, err := outbox.NewPostgresStore(db, "project_pool")
	if err != nil {
		return err
	}
	inboxStore, err := inbox.NewPostgresStore(db, "project_pool")
	if err != nil {
		return err
	}
	repo, err := projectpoolusecase.NewPostgresRepository(db, serviceName)
	if err != nil {
		return err
	}
	service := projectpoolusecase.NewService(repo)

	seed, err := projectpoolusecase.LoadSeed(ctx, cfg.ProjectPool)
	if err != nil {
		return err
	}
	if err := service.ImportSeed(ctx, seed); err != nil {
		return err
	}
	if !seed.Empty() {
		logger.Info("project pool seed imported",
			slog.Int("domains", len(seed.Domains)),
			slog.Int("projects", len(seed.Projects)),
		)
	}

	dispatcherCtx, stopDispatcher := context.WithCancel(ctx)
	defer stopDispatcher()
	dispatcher := outbox.NewDispatcher(outboxStore, bus, logger, outbox.DispatcherOptions{
		BatchSize: 25,
		Interval:  500 * time.Millisecond,
	})
	go dispatcher.Run(dispatcherCtx)

	consumer, err := natsbus.NewConsumer(ctx, cfg.NATS.URL, serviceName+"-commands", natsbus.ConsumerOptions{
		Stream:  "COMMANDS",
		Subject: "cmd.project.>",
		Queue:   serviceName,
		Durable: "project_pool_commands",
	}, inboxStore, service.Handle, logger)
	if err != nil {
		return err
	}
	defer consumer.Close()
	if err := consumer.Start(); err != nil {
		return err
	}

	logger.Info("project pool service started")
	<-ctx.Done()
	return ctx.Err()
}
