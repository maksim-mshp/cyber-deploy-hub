package projectpool

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/cloud/openstack"
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
	if seed.Empty() && cfg.ProjectPool.AutoImportOpenStackProject {
		seed, err = openStackProjectSeed(ctx, cfg)
		if err != nil {
			return err
		}
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

func openStackProjectSeed(ctx context.Context, cfg config.Config) (projectpoolusecase.Seed, error) {
	client := openstack.NewClient(cfg.OpenStack)
	info, err := client.CurrentProject(ctx)
	if err != nil {
		return projectpoolusecase.Seed{}, err
	}
	return seedFromOpenStackProject(info, cfg.ProjectPool, cfg.OpenStack.ProjectName), nil
}

func seedFromOpenStackProject(info *openstack.ProjectInfo, cfg config.ProjectPoolConfig, fallbackProjectName string) projectpoolusecase.Seed {
	domainID := strings.TrimSpace(info.DomainID)
	domainName := strings.TrimSpace(info.DomainName)
	if domainID == "" {
		domainID = strings.TrimSpace(info.DomainName)
	}
	if domainID == "" {
		domainID = strings.TrimSpace(cfg.DefaultDomainName)
	}
	if domainName == "" {
		domainName = strings.TrimSpace(cfg.DefaultDomainName)
	}
	if domainName == "" {
		domainName = domainID
	}
	courseID := strings.TrimSpace(cfg.DefaultCourseID)
	return projectpoolusecase.Seed{
		Domains: []projectpoolusecase.SeedDomain{{
			DomainID: domainID,
			CourseID: courseID,
			Name:     domainName,
		}},
		Projects: []projectpoolusecase.SeedProject{{
			ProjectID: info.ID,
			DomainID:  domainID,
			Name:      firstNonEmpty(info.Name, fallbackProjectName, info.ID),
		}},
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
