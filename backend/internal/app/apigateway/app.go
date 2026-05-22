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
	"cyber-deploy-hub/internal/usecase/authn"
	"cyber-deploy-hub/internal/usecase/labcatalog"
	"cyber-deploy-hub/internal/usecase/labs"
	"cyber-deploy-hub/internal/usecase/readmodel"
	"cyber-deploy-hub/internal/usecase/settings"
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
	var reader *readmodel.PostgresReader
	var catalogService *labcatalog.Service
	if cfg.Database.URL != "" {
		dbConfig, err := pgxpool.ParseConfig(cfg.Database.URL)
		if err != nil {
			return err
		}
		dbConfig.MaxConns = cfg.Database.MaxConns

		db, err = pgxpool.NewWithConfig(ctx, dbConfig)
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
		reader, err = readmodel.NewPostgresReader(db)
		if err != nil {
			return err
		}
		catalogRepo, err := labcatalog.NewPostgresRepository(db)
		if err != nil {
			return err
		}
		catalogService, err = labcatalog.NewService(catalogRepo)
		if err != nil {
			return err
		}
	}

	labService := labs.NewService(serviceName, bus, outboxStore)
	settingsService := settings.NewService(serviceName, bus, outboxStore)
	authService, err := authn.NewService(authn.Config{
		SessionSecret:  cfg.Auth.SessionSecret,
		SessionTTL:     cfg.Auth.SessionTTL,
		CookieName:     cfg.Auth.CookieName,
		CookieSecure:   cfg.Auth.CookieSecure,
		CookieSameSite: cfg.Auth.CookieSameSite,
		CookieDomain:   cfg.Auth.CookieDomain,
		LocalUsersJSON: cfg.Auth.LocalUsersJSON,
	})
	if err != nil {
		return err
	}
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

	server := httpapi.NewServer(labService, settingsService, catalogService, reader, authService, readiness, cloud, logger)
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
