package capacity

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/cloud/openstack"
	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/inbox"
	"cyber-deploy-hub/internal/ki"
	"cyber-deploy-hub/internal/outbox"
	natsbus "cyber-deploy-hub/internal/transport/nats"
	capacityusecase "cyber-deploy-hub/internal/usecase/capacity"
)

const serviceName = "capacity-service"

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

	outboxStore, err := outbox.NewPostgresStore(db, "capacity")
	if err != nil {
		return err
	}
	inboxStore, err := inbox.NewPostgresStore(db, "capacity")
	if err != nil {
		return err
	}
	repo, err := capacityusecase.NewPostgresRepository(db)
	if err != nil {
		return err
	}

	kiConfig := cfg.KI
	if kiConfig.Username == "" {
		kiConfig.Username = cfg.OpenStack.Username
	}
	if kiConfig.Password == "" {
		kiConfig.Password = cfg.OpenStack.Password
	}
	if kiConfig.ProjectID == "" {
		kiConfig.ProjectID = cfg.OpenStack.ProjectID
	}
	kiClient := ki.NewClient(kiConfig)
	openStackClient := openstack.NewClient(cfg.OpenStack)
	var provider capacityusecase.StatProvider = capacityusecase.NewStaticProvider(cfg.Capacity)
	var quota capacityusecase.QuotaChecker = capacityusecase.NoopQuotaChecker{}
	if kiClient.Configured() {
		provider = capacityusecase.NewKIProvider(kiClient)
	}
	if openStackClient.Configured() {
		quota = capacityusecase.NewOpenStackQuotaChecker(openStackClient)
	}
	service, err := capacityusecase.NewService(serviceName, provider, quota, repo, cfg.Capacity.ThresholdPercent)
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

	consumer, err := natsbus.NewConsumer(ctx, cfg.NATS.URL, serviceName+"-commands", natsbus.ConsumerOptions{
		Stream:  "COMMANDS",
		Subject: "cmd.capacity.>",
		Queue:   serviceName,
		Durable: "capacity_commands",
	}, inboxStore, service.Handle, logger)
	if err != nil {
		return err
	}
	defer consumer.Close()
	if err := consumer.Start(); err != nil {
		return err
	}

	logger.Info("capacity service started", slog.Float64("threshold_percent", cfg.Capacity.ThresholdPercent))
	<-ctx.Done()
	return ctx.Err()
}
