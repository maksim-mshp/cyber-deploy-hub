package lmsgateway

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/outbox"
	natsbus "cyber-deploy-hub/internal/transport/nats"
	"cyber-deploy-hub/internal/usecase/authn"
	"cyber-deploy-hub/internal/usecase/labcatalog"
	lmsusecase "cyber-deploy-hub/internal/usecase/lmsgateway"
)

const serviceName = "lms-gateway-service"

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

	outboxStore, err := outbox.NewPostgresStore(db, "lms_gateway")
	if err != nil {
		return err
	}
	repo, err := lmsusecase.NewPostgresRepository(db)
	if err != nil {
		return err
	}
	catalogRepo, err := labcatalog.NewPostgresRepository(db)
	if err != nil {
		return err
	}
	catalogService, err := labcatalog.NewService(catalogRepo)
	if err != nil {
		return err
	}
	mapper, err := lmsusecase.NewMapper(cfg.LMS.CourseMapJSON, cfg.LMS.AssignmentMapJSON)
	if err != nil {
		return err
	}
	authenticator, err := lmsusecase.NewAuthenticator(cfg.LMS.SharedSecret, cfg.LMS.AllowedClockSkew)
	if err != nil {
		return err
	}
	sessionAuth, err := authn.NewService(authn.Config{
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
	service, err := lmsusecase.NewService(serviceName, cfg.LMS.DefaultSource, mapper, repo, catalogService)
	if err != nil {
		return err
	}
	ltiService, err := NewLTIService(LTIConfig{
		PlatformIssuer:   cfg.LMS.LTIPlatformIssuer,
		ClientID:         cfg.LMS.LTIClientID,
		AuthLoginURL:     cfg.LMS.LTIAuthLoginURL,
		JWKSURL:          cfg.LMS.LTIJWKSURL,
		PublicBaseURL:    cfg.LMS.LTIPublicBaseURL,
		RedirectURL:      cfg.LMS.LTIRedirectURL,
		DeploymentIDs:    splitDeploymentIDs(cfg.LMS.LTIDeploymentIDs),
		StateSecret:      cfg.Auth.SessionSecret,
		AllowedClockSkew: cfg.LMS.AllowedClockSkew,
	}, nil)
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

	server := NewServer(service, authenticator, ltiService, sessionAuth, cfg.Auth.FrontendURL, readinessChecker{db: db, bus: bus}, logger)
	httpServer := &http.Server{
		Addr:              cfg.LMS.HTTPAddr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("lms gateway listening", slog.String("addr", cfg.LMS.HTTPAddr))
		errCh <- normalizeListenError(httpServer.ListenAndServe())
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
		if errors.Is(err, errServerClosed) {
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
