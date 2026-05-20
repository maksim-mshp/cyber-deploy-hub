package apigateway

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/cloud/openstack"
	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/outbox"
	httpapi "cyber-deploy-hub/internal/transport/http"
	natsbus "cyber-deploy-hub/internal/transport/nats"
	"cyber-deploy-hub/internal/usecase/labs"
)

const serviceName = "api-gateway-service"

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	bus, err := natsbus.NewPublisher(ctx, cfg.NATS.URL, serviceName, logger)
	if err != nil {
		return err
	}
	defer bus.Close()

	var db *pgxpool.Pool
	var outboxStore *outbox.PostgresStore
	if cfg.Database.URL != "" {
		db, err = pgxpool.New(ctx, cfg.Database.URL)
		if err != nil {
			return err
		}
		defer db.Close()

		if err := db.Ping(ctx); err != nil {
			return err
		}

		outboxStore, err = outbox.NewPostgresStore(db, "api_gateway")
		if err != nil {
			return err
		}
	}

	labService := labs.NewService(serviceName, bus, outboxStore)
	readiness := readinessChecker{db: db, bus: bus}
	cloud := openstack.NewClient(cfg.OpenStack)

	dispatcherCtx, stopDispatcher := context.WithCancel(ctx)
	defer stopDispatcher()
	if outboxStore != nil {
		dispatcher := outbox.NewDispatcher(outboxStore, bus, logger, outbox.DispatcherOptions{
			BatchSize: 25,
			Interval:  500 * time.Millisecond,
		})
		go dispatcher.Run(dispatcherCtx)
	}

	server := httpapi.NewServer(labService, readiness, cloud, logger)
	httpServer := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("api gateway listening", slog.String("addr", cfg.HTTP.Addr))
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

type readinessChecker struct {
	db  *pgxpool.Pool
	bus *natsbus.Publisher
}

func (c readinessChecker) Ready(ctx context.Context) error {
	if c.db != nil {
		if err := c.db.Ping(ctx); err != nil {
			return err
		}
	}
	if c.bus != nil {
		if err := c.bus.Ping(ctx); err != nil {
			return err
		}
	}
	return nil
}
