package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"cyber-deploy-hub/internal/app/lmsgateway"
	"cyber-deploy-hub/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", slog.Any("error", err))
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	if err := lmsgateway.Run(ctx, cfg, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("lms gateway service stopped with error", slog.Any("error", err))
		os.Exit(1)
	}
}
