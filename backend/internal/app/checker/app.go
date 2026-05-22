package checker

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/inbox"
	"cyber-deploy-hub/internal/outbox"
	natsbus "cyber-deploy-hub/internal/transport/nats"
	checkerusecase "cyber-deploy-hub/internal/usecase/checker"
	cloudadapterusecase "cyber-deploy-hub/internal/usecase/cloudadapter"
)

const serviceName = "checker-service"

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

	outboxStore, err := outbox.NewPostgresStore(db, "checker")
	if err != nil {
		return err
	}
	inboxStore, err := inbox.NewPostgresStore(db, "checker")
	if err != nil {
		return err
	}
	repo, err := checkerusecase.NewPostgresRepository(db)
	if err != nil {
		return err
	}

	profiles, err := checkerusecase.LoadProfiles(ctx, cfg.Checker)
	if err != nil {
		return err
	}
	if err := repo.SeedProfiles(ctx, profiles); err != nil {
		return err
	}

	encryptor, err := cloudadapterusecase.NewAESGCMEncryptor(cfg.Cloud)
	if err != nil {
		return err
	}
	runner := checkerusecase.NewSSHRunner(cfg.Checker.SSHTimeout, cfg.Checker.DefaultCommandTimeout)
	service, err := checkerusecase.NewService(serviceName, repo, runner, cloudKeyDecryptor{encryptor: encryptor}, cfg.Checker.SSHPort)
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
		Subject: "cmd.checker.>",
		Queue:   serviceName,
		Durable: "checker_commands",
		AckWait: checkerAckWait(profiles, cfg.Checker),
	}, inboxStore, service.Handle, logger)
	if err != nil {
		return err
	}
	defer consumer.Close()
	if err := consumer.Start(); err != nil {
		return err
	}

	logger.Info("checker service started", slog.Int("profiles", len(profiles)))
	<-ctx.Done()
	return ctx.Err()
}

type cloudKeyDecryptor struct {
	encryptor *cloudadapterusecase.AESGCMEncryptor
}

func (d cloudKeyDecryptor) Ready() error {
	return d.encryptor.Ready()
}

func (d cloudKeyDecryptor) Decrypt(ciphertext []byte, nonce []byte, keyID string) ([]byte, error) {
	return d.encryptor.Decrypt(cloudadapterusecase.EncryptedSecret{
		Ciphertext: ciphertext,
		Nonce:      nonce,
		KeyID:      keyID,
	})
}

func checkerAckWait(profiles []checkerusecase.Profile, cfg config.CheckerConfig) time.Duration {
	maxDuration := cfg.SSHTimeout + cfg.DefaultCommandTimeout
	customStepTimeout := time.Duration(checkerusecase.MaxStepTimeoutSeconds) * time.Second
	if cfg.DefaultCommandTimeout > customStepTimeout {
		customStepTimeout = cfg.DefaultCommandTimeout
	}
	customDuration := cfg.SSHTimeout + time.Duration(checkerusecase.MaxProfileSteps)*customStepTimeout
	if customDuration > maxDuration {
		maxDuration = customDuration
	}
	for _, profile := range profiles {
		profileDuration := cfg.SSHTimeout
		for _, step := range profile.Steps {
			if step.TimeoutSeconds > 0 {
				profileDuration += time.Duration(step.TimeoutSeconds) * time.Second
			} else {
				profileDuration += cfg.DefaultCommandTimeout
			}
		}
		if profileDuration > maxDuration {
			maxDuration = profileDuration
		}
	}
	return maxDuration + 30*time.Second
}
