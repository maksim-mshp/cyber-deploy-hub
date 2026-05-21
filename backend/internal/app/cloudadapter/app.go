package cloudadapter

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/cloud/openstack"
	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/inbox"
	"cyber-deploy-hub/internal/outbox"
	natsbus "cyber-deploy-hub/internal/transport/nats"
	cloudadapterusecase "cyber-deploy-hub/internal/usecase/cloudadapter"
)

const serviceName = "cloud-adapter-service"

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

	outboxStore, err := outbox.NewPostgresStore(db, "cloud_adapter")
	if err != nil {
		return err
	}
	inboxStore, err := inbox.NewPostgresStore(db, "cloud_adapter")
	if err != nil {
		return err
	}
	repo, err := cloudadapterusecase.NewPostgresRepository(db)
	if err != nil {
		return err
	}

	blueprints, err := cloudadapterusecase.LoadBlueprints(ctx, cfg.Cloud)
	if err != nil {
		return err
	}
	encryptor, err := cloudadapterusecase.NewAESGCMEncryptor(cfg.Cloud)
	if err != nil {
		return err
	}
	if err := encryptor.Ready(); err != nil {
		return err
	}

	openStackClient := openstack.NewClient(cfg.OpenStack)
	if !openStackClient.Configured() {
		return errors.New("real OpenStack provider is not configured")
	}
	if strings.TrimSpace(cfg.Cloud.PrivateSubnetID) == "" {
		return errors.New("CLOUD_PRIVATE_SUBNET_ID is required")
	}
	provider := cloudadapterusecase.NewOpenStackProvider(openStackClient, cfg.Cloud)
	service, err := cloudadapterusecase.NewService(serviceName, provider, repo, encryptor, blueprints)
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
		Stream:        "COMMANDS",
		Subject:       "cmd.cloud.>",
		Queue:         serviceName,
		Durable:       "cloud_adapter_commands",
		AckWait:       2 * time.Minute,
		MaxAckPending: 1,
	}, inboxStore, service.Handle, logger)
	if err != nil {
		return err
	}
	defer consumer.Close()
	if err := consumer.Start(); err != nil {
		return err
	}

	logger.Info("cloud adapter service started", slog.Int("blueprints", len(blueprints)))
	<-ctx.Done()
	return ctx.Err()
}
