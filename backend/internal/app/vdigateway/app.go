package vdigateway

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/contracts/events"
	"cyber-deploy-hub/internal/inbox"
	"cyber-deploy-hub/internal/outbox"
	natsbus "cyber-deploy-hub/internal/transport/nats"
	vdigatewayusecase "cyber-deploy-hub/internal/usecase/vdigateway"
)

const serviceName = "vdi-gateway-service"

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

	outboxStore, err := outbox.NewPostgresStore(db, "vdi_gateway")
	if err != nil {
		return err
	}
	inboxStore, err := inbox.NewPostgresStore(db, "vdi_gateway")
	if err != nil {
		return err
	}
	repo, err := vdigatewayusecase.NewPostgresRepository(db)
	if err != nil {
		return err
	}
	service, err := vdigatewayusecase.NewService(serviceName, repo, cfg.VDI)
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

	consumers, err := startConsumers(ctx, cfg, inboxStore, service, logger)
	if err != nil {
		return err
	}
	defer func() {
		for _, consumer := range consumers {
			consumer.Close()
		}
	}()

	readiness := readinessChecker{db: db, bus: bus}
	httpServer := &http.Server{
		Addr:              cfg.VDI.HTTPAddr,
		Handler:           newHTTPServer(service, readiness, logger).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("vdi gateway listening", slog.String("addr", cfg.VDI.HTTPAddr))
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func startConsumers(ctx context.Context, cfg config.Config, inboxStore *inbox.PostgresStore, service *vdigatewayusecase.Service, logger *slog.Logger) ([]*natsbus.Consumer, error) {
	specs := []struct {
		name    string
		stream  string
		subject string
		durable string
	}{
		{name: "commands", stream: "COMMANDS", subject: "cmd.vdi.>", durable: "vdi_gateway_commands"},
		{name: "lifecycle-frozen", stream: "EVENTS", subject: events.LifecycleLabFrozenV1.String(), durable: "vdi_gateway_lifecycle_frozen"},
		{name: "cloud-cleaned", stream: "EVENTS", subject: events.CloudLabCleanedV1.String(), durable: "vdi_gateway_cloud_cleaned"},
		{name: "lab-failed", stream: "EVENTS", subject: events.LabFailedV1.String(), durable: "vdi_gateway_lab_failed"},
	}

	consumers := make([]*natsbus.Consumer, 0, len(specs))
	for _, spec := range specs {
		consumer, err := natsbus.NewConsumer(ctx, cfg.NATS.URL, serviceName+"-"+spec.name, natsbus.ConsumerOptions{
			Stream:  spec.stream,
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

type readinessChecker struct {
	db  *pgxpool.Pool
	bus *natsbus.Publisher
}

func (c readinessChecker) Ready(ctx context.Context) error {
	if err := c.db.Ping(ctx); err != nil {
		return err
	}
	return c.bus.Ping(ctx)
}

func remoteAddr(r *http.Request) string {
	forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwarded != "" {
		return strings.Split(forwarded, ",")[0]
	}
	return r.RemoteAddr
}
