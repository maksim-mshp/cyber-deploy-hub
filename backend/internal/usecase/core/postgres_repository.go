package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/domain"
)

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) (*PostgresRepository, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	return &PostgresRepository{db: db}, nil
}

func (r *PostgresRepository) StartProvisioning(ctx context.Context, req commands.RequestProvisionV1Payload, command contracts.Envelope, next contracts.Envelope) error {
	resources, err := json.Marshal(req.Resources)
	if err != nil {
		return err
	}
	instances, err := json.Marshal(req.Instances)
	if err != nil {
		return err
	}
	return r.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
INSERT INTO core.lab_runs (id, student_id, course_id, lab_id, state, resources, instances)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO NOTHING`,
			req.LabRunID, req.StudentID, req.CourseID, req.LabID, string(domain.LabRunRequested), resources, instances); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
INSERT INTO core.saga_instances (id, lab_run_id, saga_type, state)
VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO NOTHING`,
			command.SagaID, req.LabRunID, sagaTypeProvision, string(domain.LabRunRequested)); err != nil {
			return err
		}

		if err := insertStep(ctx, tx, command.SagaID, "request_provision", "DONE", command.MessageID, ""); err != nil {
			return err
		}
		if err := insertLabRunEvent(ctx, tx, req.LabRunID, domain.LabRunRequested, command); err != nil {
			return err
		}

		tag, err := tx.Exec(ctx, `
UPDATE core.lab_runs
SET state = $2,
    updated_at = now()
WHERE id = $1`,
			req.LabRunID, string(domain.LabRunAllocatingProject))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("lab_run %s not found", req.LabRunID)
		}

		if _, err := tx.Exec(ctx, `
UPDATE core.saga_instances
SET state = $2,
    updated_at = now()
WHERE id = $1`,
			command.SagaID, string(domain.LabRunAllocatingProject)); err != nil {
			return err
		}

		if err := insertStep(ctx, tx, command.SagaID, "allocate_project", "PENDING", next.MessageID, ""); err != nil {
			return err
		}
		if err := insertLabRunEvent(ctx, tx, req.LabRunID, domain.LabRunAllocatingProject, command); err != nil {
			return err
		}
		return insertOutbox(ctx, tx, next)
	})
}

func (r *PostgresRepository) Advance(ctx context.Context, transition Transition) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		if len(transition.ExpectedStates) > 0 {
			allowed, err := lockAndCheckState(ctx, tx, transition.LabRunID, transition.ExpectedStates)
			if err != nil {
				return err
			}
			if !allowed {
				return nil
			}
		}
		tag, err := tx.Exec(ctx, `
UPDATE core.lab_runs
SET state = $2,
    project_id = COALESCE(NULLIF($3, ''), project_id),
    vdi_access_url = COALESCE(NULLIF($4, ''), vdi_access_url),
    updated_at = now()
WHERE id = $1`,
			transition.LabRunID, string(transition.State), transition.ProjectID, transition.VDIURL)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("lab_run %s not found", transition.LabRunID)
		}

		if transition.Message.SagaID != "" {
			if _, err := tx.Exec(ctx, `
UPDATE core.saga_instances
SET state = $2,
    updated_at = now()
WHERE id = $1`,
				transition.Message.SagaID, string(transition.State)); err != nil {
				return err
			}

			if transition.StepName != "" {
				if err := insertStep(ctx, tx, transition.Message.SagaID, transition.StepName, "DONE", transition.Message.MessageID, ""); err != nil {
					return err
				}
			}
		}
		if err := insertLabRunEvent(ctx, tx, transition.LabRunID, transition.State, transition.Message); err != nil {
			return err
		}
		for _, next := range transition.Next {
			if err := insertOutbox(ctx, tx, next); err != nil {
				return err
			}
		}
		return nil
	})
}

func lockAndCheckState(ctx context.Context, tx pgx.Tx, labRunID string, expected []domain.LabRunState) (bool, error) {
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM core.lab_runs WHERE id = $1 FOR UPDATE`, labRunID).Scan(&state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("lab_run %s not found", labRunID)
		}
		return false, err
	}
	for _, item := range expected {
		if state == string(item) {
			return true, nil
		}
	}
	return false, nil
}

func (r *PostgresRepository) Fail(ctx context.Context, failure Failure) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
UPDATE core.lab_runs
SET state = $2,
    failure_code = $3,
    failure_message = $4,
    updated_at = now()
WHERE id = $1`,
			failure.LabRunID, string(domain.LabRunFailed), failure.Code, failure.Message)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("lab_run %s not found", failure.LabRunID)
		}

		if failure.Event.SagaID != "" {
			if _, err := tx.Exec(ctx, `
UPDATE core.saga_instances
SET state = $2,
    updated_at = now()
WHERE id = $1`,
				failure.Event.SagaID, string(domain.LabRunFailed)); err != nil {
				return err
			}

			if failure.StepName != "" {
				if err := insertStep(ctx, tx, failure.Event.SagaID, failure.StepName, "FAILED", failure.Event.MessageID, failure.Message); err != nil {
					return err
				}
			}
		}
		if err := insertLabRunEvent(ctx, tx, failure.LabRunID, domain.LabRunFailed, failure.Event); err != nil {
			return err
		}
		for _, next := range failure.Next {
			if err := insertOutbox(ctx, tx, next); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *PostgresRepository) LoadLabRun(ctx context.Context, labRunID string) (LabRun, error) {
	var labRun LabRun
	var state string
	var rawResources []byte
	var rawInstances []byte
	err := r.db.QueryRow(ctx, `
SELECT id::text,
       student_id,
       course_id,
       lab_id,
       COALESCE(project_id, ''),
       state,
       COALESCE(resources, '{}'::jsonb),
       COALESCE(instances, '[]'::jsonb)
FROM core.lab_runs
WHERE id = $1`, labRunID).Scan(
		&labRun.ID,
		&labRun.StudentID,
		&labRun.CourseID,
		&labRun.LabID,
		&labRun.ProjectID,
		&state,
		&rawResources,
		&rawInstances,
	)
	if err != nil {
		return LabRun{}, err
	}
	if err := json.Unmarshal(rawResources, &labRun.Resources); err != nil {
		return LabRun{}, fmt.Errorf("decode lab resources: %w", err)
	}
	if err := json.Unmarshal(rawInstances, &labRun.Instances); err != nil {
		return LabRun{}, fmt.Errorf("decode lab instances: %w", err)
	}
	labRun.State = domain.LabRunState(state)
	return labRun, nil
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

func insertStep(ctx context.Context, tx pgx.Tx, sagaID string, name string, state string, messageID string, errMessage string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO core.saga_steps (saga_id, step_name, state, message_id, error_message)
VALUES ($1, $2, $3, NULLIF($4, '')::uuid, NULLIF($5, ''))`,
		sagaID, name, state, messageID, errMessage)
	return err
}

func insertLabRunEvent(ctx context.Context, tx pgx.Tx, labRunID string, state domain.LabRunState, envelope contracts.Envelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO core.lab_run_events (lab_run_id, state, message_id, message_type, payload)
VALUES ($1, $2, $3, $4, $5)`,
		labRunID, string(state), envelope.MessageID, envelope.MessageType, payload)
	return err
}

func insertOutbox(ctx context.Context, tx pgx.Tx, envelope contracts.Envelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO core.outbox (message_id, message_kind, subject, payload, status)
VALUES ($1, $2, $3, $4, 'PENDING')
ON CONFLICT (message_id) DO NOTHING`,
		envelope.MessageID, string(envelope.MessageKind), envelope.MessageType, payload)
	return err
}
