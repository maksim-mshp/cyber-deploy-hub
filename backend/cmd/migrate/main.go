package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	var (
		databaseURL = flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL connection URL")
		path        = flag.String("path", "file://migrations", "go-migrate source URL")
		action      = flag.String("action", "up", "migration action: up, down, force")
		version     = flag.Int("version", 0, "version for force action")
	)
	flag.Parse()

	if *databaseURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	m, err := migrate.New(*path, *databaseURL)
	if err != nil {
		slog.Error("failed to initialize migrator", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() {
		sourceErr, databaseErr := m.Close()
		if sourceErr != nil {
			slog.Error("failed to close migration source", slog.Any("error", sourceErr))
		}
		if databaseErr != nil {
			slog.Error("failed to close migration database", slog.Any("error", databaseErr))
		}
	}()

	if err := run(m, *action, *version); err != nil {
		slog.Error("migration failed", slog.String("action", *action), slog.Any("error", err))
		os.Exit(1)
	}

	fmt.Printf("migration %s completed\n", *action)
}

func run(m *migrate.Migrate, action string, version int) error {
	switch action {
	case "up":
		err := m.Up()
		if errors.Is(err, migrate.ErrNoChange) {
			return nil
		}
		return err
	case "down":
		err := m.Steps(-1)
		if errors.Is(err, migrate.ErrNoChange) {
			return nil
		}
		return err
	case "force":
		return m.Force(version)
	default:
		return fmt.Errorf("unsupported action %q", action)
	}
}
