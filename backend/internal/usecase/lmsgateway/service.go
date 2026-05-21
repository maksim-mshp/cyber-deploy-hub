package lmsgateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/usecase/labcatalog"
)

type Repository interface {
	SaveLaunch(ctx context.Context, launch LaunchRecord, command contracts.Envelope) (LaunchRecord, bool, error)
	LoadResult(ctx context.Context, launchID string) (LaunchResult, bool, error)
}

type LabCatalog interface {
	Get(ctx context.Context, courseID string, labID string) (labcatalog.Definition, bool, error)
}

type Service struct {
	producer string
	source   string
	mapper   Mapper
	repo     Repository
	catalog  LabCatalog
}

func NewService(producer string, source string, mapper Mapper, repo Repository, catalog LabCatalog) (*Service, error) {
	if strings.TrimSpace(producer) == "" {
		return nil, errors.New("producer is empty")
	}
	if strings.TrimSpace(source) == "" {
		source = "moodle"
	}
	if repo == nil {
		return nil, errors.New("repository is nil")
	}
	if catalog == nil {
		return nil, errors.New("lab catalog is nil")
	}
	return &Service{producer: producer, source: source, mapper: mapper, repo: repo, catalog: catalog}, nil
}

func (s *Service) Launch(ctx context.Context, req LaunchRequest) (LaunchAccepted, bool, error) {
	mapping, err := s.mapper.Map(req)
	if err != nil {
		return LaunchAccepted{}, false, err
	}
	definition, found, err := s.catalog.Get(ctx, mapping.CourseID, mapping.LabID)
	if err != nil {
		return LaunchAccepted{}, false, err
	}
	if !found || !definition.Enabled {
		return LaunchAccepted{}, false, errors.New("lab definition is not available")
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = defaultIdempotencyKey(req)
	}

	labRunID := uuid.NewString()
	sagaID := uuid.NewString()
	command, err := contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:           contracts.MessageKindCommand,
		Type:           commands.RequestProvisionV1,
		Producer:       s.producer,
		SagaID:         sagaID,
		AggregateType:  "lab_run",
		AggregateID:    labRunID,
		IdempotencyKey: idempotencyKey,
		Payload: commands.RequestProvisionV1Payload{
			LabRunID:  labRunID,
			StudentID: mapping.StudentID,
			CourseID:  mapping.CourseID,
			LabID:     mapping.LabID,
			Source:    s.source,
			Resources: definition.Resources,
			Instances: append([]commands.VMBlueprint(nil), definition.Instances...),
		},
	})
	if err != nil {
		return LaunchAccepted{}, false, err
	}

	launch := LaunchRecord{
		ID:                   uuid.NewString(),
		IdempotencyKey:       idempotencyKey,
		ExternalUserID:       strings.TrimSpace(req.MoodleUserID),
		ExternalCourseID:     strings.TrimSpace(req.MoodleCourseID),
		ExternalAssignmentID: strings.TrimSpace(req.MoodleAssignmentID),
		ExternalUserLogin:    strings.TrimSpace(req.UserLogin),
		ExternalCourseName:   strings.TrimSpace(req.CourseName),
		LocalStudentID:       mapping.StudentID,
		LocalCourseID:        mapping.CourseID,
		LocalLabID:           mapping.LabID,
		LabRunID:             labRunID,
		SagaID:               sagaID,
		CommandID:            command.MessageID,
		Status:               "ACCEPTED",
		RequestPayload:       rawJSON(req),
	}

	saved, inserted, err := s.repo.SaveLaunch(ctx, launch, command)
	if err != nil {
		return LaunchAccepted{}, false, err
	}
	if !inserted && saved.Status == "ACCEPTED" {
		saved.Status = "ALREADY_ACCEPTED"
	}
	return saved.Accepted(), inserted, nil
}

func (s *Service) MappingDescription() MappingDescription {
	return s.mapper.Description()
}

func (s *Service) Result(ctx context.Context, launchID string) (LaunchResult, bool, error) {
	launchID = strings.TrimSpace(launchID)
	if launchID == "" {
		return LaunchResult{}, false, errors.New("launch_id is required")
	}
	return s.repo.LoadResult(ctx, launchID)
}

func defaultIdempotencyKey(req LaunchRequest) string {
	return strings.Join([]string{
		"moodle",
		strings.TrimSpace(req.MoodleUserID),
		strings.TrimSpace(req.MoodleCourseID),
		strings.TrimSpace(req.MoodleAssignmentID),
	}, ":")
}

func rawJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}
