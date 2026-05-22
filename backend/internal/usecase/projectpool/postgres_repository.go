package projectpool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/contracts/events"
)

const (
	aggregateTypeLabRun = "lab_run"

	projectStateFree        = "FREE"
	projectStateAllocated   = "ALLOCATED"
	projectStateQuarantined = "QUARANTINED"

	allocationStateActive      = "ACTIVE"
	allocationStateReleased    = "RELEASED"
	allocationStateQuarantined = "QUARANTINED"
)

type PostgresRepository struct {
	db       *pgxpool.Pool
	producer string
}

type allocatedProject struct {
	ID       string
	DomainID string
	Name     string
	State    string
}

func NewPostgresRepository(db *pgxpool.Pool, producer string) (*PostgresRepository, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	if strings.TrimSpace(producer) == "" {
		return nil, errors.New("producer is empty")
	}
	return &PostgresRepository{db: db, producer: producer}, nil
}

func (r *PostgresRepository) ImportSeed(ctx context.Context, seed Seed) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		for _, domain := range seed.Domains {
			if _, err := tx.Exec(ctx, `
INSERT INTO project_pool.domains (domain_id, course_id, name)
VALUES ($1, $2, $3)
ON CONFLICT (domain_id) DO UPDATE
SET course_id = EXCLUDED.course_id,
    name = EXCLUDED.name,
    updated_at = now()`,
				domain.DomainID, domain.CourseID, domain.Name); err != nil {
				return err
			}
		}

		for _, project := range seed.Projects {
			if _, err := tx.Exec(ctx, `
INSERT INTO project_pool.ki_projects (id, domain_id, name, state)
VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO UPDATE
SET domain_id = EXCLUDED.domain_id,
    name = EXCLUDED.name,
    updated_at = now()`,
				project.ProjectID, project.DomainID, project.Name, projectStateFree); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *PostgresRepository) HasProjects(ctx context.Context) (bool, error) {
	var hasProjects bool
	if err := r.db.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM project_pool.ki_projects
)`).Scan(&hasProjects); err != nil {
		return false, err
	}
	return hasProjects, nil
}

func (r *PostgresRepository) Allocate(ctx context.Context, command contracts.Envelope, req commands.ProjectAllocateV1Payload) error {
	if err := validateAllocateRequest(req); err != nil {
		return err
	}

	return r.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`, req.StudentID, req.CourseID); err != nil {
			return err
		}

		existing, found, err := findActiveStudentProject(ctx, tx, req.StudentID, req.CourseID)
		if err != nil {
			return err
		}
		if found {
			event, err := r.projectAllocatedEvent(command, req.LabRunID, existing.ID, existing.DomainID)
			if err != nil {
				return err
			}
			return insertOutbox(ctx, tx, event)
		}

		project, found, err := allocateFreeProject(ctx, tx, req)
		if err != nil {
			return err
		}
		if !found {
			message := fmt.Sprintf("no free project for course %s", req.CourseID)
			event, err := r.projectAllocationFailedEvent(command, req.LabRunID, "PROJECT_POOL_EXHAUSTED", message)
			if err != nil {
				return err
			}
			if err := insertHistory(ctx, tx, "", req.LabRunID, "", "EXHAUSTED", message, command.MessageID); err != nil {
				return err
			}
			return insertOutbox(ctx, tx, event)
		}

		if err := insertAllocation(ctx, tx, req, project.ID); err != nil {
			return err
		}
		if err := insertHistory(ctx, tx, project.ID, req.LabRunID, projectStateFree, projectStateAllocated, "allocated", command.MessageID); err != nil {
			return err
		}

		event, err := r.projectAllocatedEvent(command, req.LabRunID, project.ID, project.DomainID)
		if err != nil {
			return err
		}
		return insertOutbox(ctx, tx, event)
	})
}

func (r *PostgresRepository) Release(ctx context.Context, command contracts.Envelope, req commands.ProjectReleaseV1Payload) error {
	if err := validateReleaseRequest(req); err != nil {
		return err
	}

	return r.tx(ctx, func(tx pgx.Tx) error {
		project, err := lockProject(ctx, tx, req.ProjectID)
		if err != nil {
			return err
		}

		nextProjectState := projectStateFree
		nextAllocationState := allocationStateReleased
		if isQuarantineReason(req.Reason) {
			nextProjectState = projectStateQuarantined
			nextAllocationState = allocationStateQuarantined
		}

		if _, err := tx.Exec(ctx, `
UPDATE project_pool.ki_projects
SET state = $2,
    current_lab_run_id = NULL,
    reserved_by_student_id = NULL,
    reserved_until = NULL,
    updated_at = now()
WHERE id = $1`,
			req.ProjectID, nextProjectState); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE project_pool.allocations
SET state = $2,
    released_at = now(),
    release_reason = NULLIF($3, ''),
    updated_at = now()
WHERE project_id = $1
  AND state = 'ACTIVE'`,
			req.ProjectID, nextAllocationState, req.Reason); err != nil {
			return err
		}
		if err := insertHistory(ctx, tx, req.ProjectID, req.LabRunID, project.State, nextProjectState, releaseReason(req.Reason), command.MessageID); err != nil {
			return err
		}

		event, err := r.projectReleasedEvent(command, req, nextProjectState)
		if err != nil {
			return err
		}
		return insertOutbox(ctx, tx, event)
	})
}

func validateAllocateRequest(req commands.ProjectAllocateV1Payload) error {
	if strings.TrimSpace(req.LabRunID) == "" {
		return errors.New("lab_run_id is required")
	}
	if strings.TrimSpace(req.StudentID) == "" {
		return errors.New("student_id is required")
	}
	if strings.TrimSpace(req.CourseID) == "" {
		return errors.New("course_id is required")
	}
	if strings.TrimSpace(req.LabID) == "" {
		return errors.New("lab_id is required")
	}
	return nil
}

func validateReleaseRequest(req commands.ProjectReleaseV1Payload) error {
	if strings.TrimSpace(req.LabRunID) == "" {
		return errors.New("lab_run_id is required")
	}
	if _, err := uuid.Parse(req.ProjectID); err != nil {
		return fmt.Errorf("project_id must be uuid: %w", err)
	}
	return nil
}

func findActiveStudentProject(ctx context.Context, tx pgx.Tx, studentID string, courseID string) (allocatedProject, bool, error) {
	var project allocatedProject
	err := tx.QueryRow(ctx, `
SELECT p.id::text, p.domain_id, p.name, p.state
FROM project_pool.allocations AS a
JOIN project_pool.ki_projects AS p ON p.id = a.project_id
WHERE a.student_id = $1
  AND a.course_id = $2
  AND a.state = 'ACTIVE'
ORDER BY a.allocated_at
LIMIT 1`,
		studentID, courseID).Scan(&project.ID, &project.DomainID, &project.Name, &project.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return allocatedProject{}, false, nil
	}
	if err != nil {
		return allocatedProject{}, false, err
	}
	return project, true, nil
}

func allocateFreeProject(ctx context.Context, tx pgx.Tx, req commands.ProjectAllocateV1Payload) (allocatedProject, bool, error) {
	var project allocatedProject
	err := tx.QueryRow(ctx, `
WITH candidate AS (
    SELECT p.id
    FROM project_pool.ki_projects AS p
    JOIN project_pool.domains AS d ON d.domain_id = p.domain_id
    WHERE p.state = 'FREE'
      AND (($4 <> '' AND p.domain_id = $4) OR ($4 = '' AND d.course_id = $3))
    ORDER BY p.updated_at, p.id
    LIMIT 1
    FOR UPDATE OF p SKIP LOCKED
)
UPDATE project_pool.ki_projects AS p
SET state = 'ALLOCATED',
    current_lab_run_id = $1,
    reserved_by_student_id = $2,
    reserved_until = NULL,
    updated_at = now()
FROM candidate
WHERE p.id = candidate.id
RETURNING p.id::text, p.domain_id, p.name, p.state`,
		req.LabRunID, req.StudentID, req.CourseID, req.RequestedDomain).Scan(
		&project.ID,
		&project.DomainID,
		&project.Name,
		&project.State,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return allocatedProject{}, false, nil
	}
	if err != nil {
		return allocatedProject{}, false, err
	}
	return project, true, nil
}

func insertAllocation(ctx context.Context, tx pgx.Tx, req commands.ProjectAllocateV1Payload, projectID string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO project_pool.allocations (lab_run_id, project_id, student_id, course_id, lab_id, state)
VALUES ($1, $2, $3, $4, $5, 'ACTIVE')
ON CONFLICT (lab_run_id) DO UPDATE
SET project_id = EXCLUDED.project_id,
    student_id = EXCLUDED.student_id,
    course_id = EXCLUDED.course_id,
    lab_id = EXCLUDED.lab_id,
    state = 'ACTIVE',
    released_at = NULL,
    release_reason = NULL,
    updated_at = now()`,
		req.LabRunID, projectID, req.StudentID, req.CourseID, req.LabID)
	return err
}

func lockProject(ctx context.Context, tx pgx.Tx, projectID string) (allocatedProject, error) {
	var project allocatedProject
	err := tx.QueryRow(ctx, `
SELECT id::text, domain_id, name, state
FROM project_pool.ki_projects
WHERE id = $1
FOR UPDATE`,
		projectID).Scan(&project.ID, &project.DomainID, &project.Name, &project.State)
	if err != nil {
		return allocatedProject{}, err
	}
	return project, nil
}

func insertHistory(ctx context.Context, tx pgx.Tx, projectID string, labRunID string, fromState string, toState string, reason string, messageID string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO project_pool.state_history (project_id, lab_run_id, from_state, to_state, reason, message_id)
VALUES (NULLIF($1, '')::uuid, NULLIF($2, '')::uuid, NULLIF($3, ''), $4, NULLIF($5, ''), NULLIF($6, '')::uuid)`,
		projectID, labRunID, fromState, toState, reason, messageID)
	return err
}

func insertOutbox(ctx context.Context, tx pgx.Tx, envelope contracts.Envelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO project_pool.outbox (message_id, message_kind, subject, payload, status)
VALUES ($1, $2, $3, $4, 'PENDING')
ON CONFLICT (message_id) DO NOTHING`,
		envelope.MessageID, string(envelope.MessageKind), envelope.MessageType, payload)
	return err
}

func (r *PostgresRepository) projectAllocatedEvent(command contracts.Envelope, labRunID string, projectID string, domainID string) (contracts.Envelope, error) {
	return r.newEvent(command, events.ProjectAllocatedV1, events.ProjectAllocatedV1Payload{
		LabRunID:  labRunID,
		ProjectID: projectID,
		DomainID:  domainID,
	})
}

func (r *PostgresRepository) projectAllocationFailedEvent(command contracts.Envelope, labRunID string, code string, message string) (contracts.Envelope, error) {
	envelope, err := r.newEvent(command, events.ProjectAllocationFailedV1, events.FailurePayload{
		LabRunID: labRunID,
		Code:     code,
		Message:  message,
	})
	if err != nil {
		return contracts.Envelope{}, err
	}
	envelope.Error = &contracts.MessageError{Code: code, Message: message}
	return envelope, nil
}

func (r *PostgresRepository) projectReleasedEvent(command contracts.Envelope, req commands.ProjectReleaseV1Payload, state string) (contracts.Envelope, error) {
	return r.newEvent(command, events.ProjectReleasedV1, events.ProjectReleasedV1Payload{
		LabRunID:  req.LabRunID,
		ProjectID: req.ProjectID,
		State:     state,
		Reason:    req.Reason,
	})
}

func (r *PostgresRepository) newEvent(cause contracts.Envelope, subject contracts.Subject, payload any) (contracts.Envelope, error) {
	envelope, err := contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:           contracts.MessageKindEvent,
		Type:           subject,
		Producer:       r.producer,
		CorrelationID:  cause.CorrelationID,
		CausationID:    cause.MessageID,
		SagaID:         cause.SagaID,
		AggregateType:  aggregateTypeLabRun,
		AggregateID:    cause.AggregateID,
		IdempotencyKey: cause.SagaID + ":" + subject.String(),
		Payload:        payload,
	})
	if err != nil {
		return contracts.Envelope{}, err
	}
	envelope.MessageID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(cause.MessageID+":"+subject.String())).String()
	return envelope, nil
}

func isQuarantineReason(reason string) bool {
	normalized := strings.ToLower(strings.TrimSpace(reason))
	return strings.Contains(normalized, "cleanup_failed") ||
		strings.Contains(normalized, "cleanup_error") ||
		strings.Contains(normalized, "quarantine")
}

func releaseReason(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return "released"
	}
	return reason
}

func (r *PostgresRepository) tx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
