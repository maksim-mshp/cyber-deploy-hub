package lifecycle

import (
	"context"
	"log/slog"
	"time"
)

type Scheduler struct {
	service      *Service
	logger       *slog.Logger
	pollInterval time.Duration
	batchSize    int
}

func NewScheduler(service *Service, logger *slog.Logger, pollInterval time.Duration, batchSize int) *Scheduler {
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 25
	}
	return &Scheduler{
		service:      service,
		logger:       logger,
		pollInterval: pollInterval,
		batchSize:    batchSize,
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		s.fireDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) fireDue(ctx context.Context) {
	fired, err := s.service.FireDueTimers(ctx, s.batchSize)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("failed to fire lifecycle timers", slog.Any("error", err))
		}
		return
	}
	if fired > 0 && s.logger != nil {
		s.logger.Info("lifecycle timers fired", slog.Int("count", fired))
	}
}
