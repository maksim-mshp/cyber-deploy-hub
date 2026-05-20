package projectpool

import (
	"context"
	"encoding/json"
	"fmt"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
)

type Repository interface {
	ImportSeed(ctx context.Context, seed Seed) error
	Allocate(ctx context.Context, command contracts.Envelope, req commands.ProjectAllocateV1Payload) error
	Release(ctx context.Context, command contracts.Envelope, req commands.ProjectReleaseV1Payload) error
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ImportSeed(ctx context.Context, seed Seed) error {
	if seed.Empty() {
		return nil
	}
	return s.repo.ImportSeed(ctx, seed)
}

func (s *Service) Handle(ctx context.Context, envelope contracts.Envelope) error {
	switch envelope.MessageType {
	case commands.ProjectAllocateV1.String():
		var payload commands.ProjectAllocateV1Payload
		if err := decodePayload(envelope, &payload); err != nil {
			return err
		}
		return s.repo.Allocate(ctx, envelope, payload)
	case commands.ProjectReleaseV1.String():
		var payload commands.ProjectReleaseV1Payload
		if err := decodePayload(envelope, &payload); err != nil {
			return err
		}
		return s.repo.Release(ctx, envelope, payload)
	default:
		return nil
	}
}

func decodePayload(envelope contracts.Envelope, dst any) error {
	if err := json.Unmarshal(envelope.Payload, dst); err != nil {
		return fmt.Errorf("decode %s payload: %w", envelope.MessageType, err)
	}
	return nil
}
