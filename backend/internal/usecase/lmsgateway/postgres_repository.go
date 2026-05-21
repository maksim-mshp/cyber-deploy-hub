package lmsgateway

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/contracts"
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

func (r *PostgresRepository) SaveLaunch(ctx context.Context, launch LaunchRecord, command contracts.Envelope) (LaunchRecord, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return LaunchRecord{}, false, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := lockStudentLaunch(ctx, tx, launch.LocalStudentID); err != nil {
		return LaunchRecord{}, false, err
	}

	existing, found, err := loadLaunchByIdempotencyKey(ctx, tx, launch.IdempotencyKey)
	if err != nil {
		return LaunchRecord{}, false, err
	}
	if found {
		if err := tx.Commit(ctx); err != nil {
			return LaunchRecord{}, false, err
		}
		return existing, false, nil
	}

	blockingLabRunID, found, err := findBlockingLabRunID(ctx, tx, launch.LocalStudentID)
	if err != nil {
		return LaunchRecord{}, false, err
	}
	if found {
		launch.LabRunID = blockingLabRunID
		launch.Status = LaunchStatusActiveLabExists
		if err := insertLaunch(ctx, tx, launch); err != nil {
			return LaunchRecord{}, false, err
		}
		saved, found, err := loadLaunchByIdempotencyKey(ctx, tx, launch.IdempotencyKey)
		if err != nil {
			return LaunchRecord{}, false, err
		}
		if !found {
			return LaunchRecord{}, false, errors.New("launch was not saved")
		}
		if err := tx.Commit(ctx); err != nil {
			return LaunchRecord{}, false, err
		}
		return saved, false, nil
	}

	if err := insertLaunch(ctx, tx, launch); err != nil {
		return LaunchRecord{}, false, err
	}
	if err := insertOutbox(ctx, tx, command); err != nil {
		return LaunchRecord{}, false, err
	}
	inserted, found, err := loadLaunchByIdempotencyKey(ctx, tx, launch.IdempotencyKey)
	if err != nil {
		return LaunchRecord{}, false, err
	}
	if !found {
		return LaunchRecord{}, false, errors.New("launch was not saved")
	}
	if err := tx.Commit(ctx); err != nil {
		return LaunchRecord{}, false, err
	}
	return inserted, true, nil
}

func insertLaunch(ctx context.Context, tx pgx.Tx, launch LaunchRecord) error {
	_, err := tx.Exec(ctx, `
INSERT INTO lms_gateway.launches (
    id,
    idempotency_key,
    external_user_id,
    external_course_id,
    external_assignment_id,
    external_user_login,
    external_course_name,
    local_student_id,
    local_course_id,
    local_lab_id,
    lab_run_id,
    saga_id,
    command_id,
    status,
    request_payload
)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT (idempotency_key) DO NOTHING`,
		launch.ID,
		launch.IdempotencyKey,
		launch.ExternalUserID,
		launch.ExternalCourseID,
		launch.ExternalAssignmentID,
		launch.ExternalUserLogin,
		launch.ExternalCourseName,
		launch.LocalStudentID,
		launch.LocalCourseID,
		launch.LocalLabID,
		launch.LabRunID,
		launch.SagaID,
		launch.CommandID,
		launch.Status,
		launch.RequestPayload,
	)
	return err
}

func (r *PostgresRepository) LoadResult(ctx context.Context, launchID string) (LaunchResult, bool, error) {
	var result LaunchResult
	var checkPassed *bool
	err := r.db.QueryRow(ctx, `
SELECT l.id::text,
       l.lab_run_id::text,
       l.status,
       COALESCE(lr.state, ''),
       COALESCE(lr.failure_code, ''),
       COALESCE(lr.failure_message, ''),
       COALESCE(cr.state, ''),
       cr.passed,
       COALESCE(cr.error_message, ''),
       l.local_student_id,
       l.local_course_id,
       l.local_lab_id
FROM lms_gateway.launches l
LEFT JOIN core.lab_runs lr ON lr.id = l.lab_run_id
LEFT JOIN LATERAL (
    SELECT state, passed, error_message
    FROM checker.runs
    WHERE lab_run_id = l.lab_run_id
    ORDER BY finished_at DESC
    LIMIT 1
) cr ON true
WHERE l.id = $1`, launchID).Scan(
		&result.LaunchID,
		&result.LabRunID,
		&result.Status,
		&result.LabState,
		&result.FailureCode,
		&result.FailureMessage,
		&result.CheckState,
		&checkPassed,
		&result.CheckError,
		&result.Mapping.StudentID,
		&result.Mapping.CourseID,
		&result.Mapping.LabID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return LaunchResult{}, false, nil
	}
	if err != nil {
		return LaunchResult{}, false, err
	}
	result.CheckPassed = checkPassed
	return result, true, nil
}

func loadLaunchByIdempotencyKey(ctx context.Context, tx pgx.Tx, key string) (LaunchRecord, bool, error) {
	var launch LaunchRecord
	err := tx.QueryRow(ctx, `
SELECT id::text,
       idempotency_key,
       external_user_id,
       external_course_id,
       external_assignment_id,
       COALESCE(external_user_login, ''),
       COALESCE(external_course_name, ''),
       local_student_id,
       local_course_id,
       local_lab_id,
       lab_run_id::text,
       saga_id::text,
       command_id::text,
       status,
       request_payload,
       created_at
FROM lms_gateway.launches
WHERE idempotency_key = $1`, key).Scan(
		&launch.ID,
		&launch.IdempotencyKey,
		&launch.ExternalUserID,
		&launch.ExternalCourseID,
		&launch.ExternalAssignmentID,
		&launch.ExternalUserLogin,
		&launch.ExternalCourseName,
		&launch.LocalStudentID,
		&launch.LocalCourseID,
		&launch.LocalLabID,
		&launch.LabRunID,
		&launch.SagaID,
		&launch.CommandID,
		&launch.Status,
		&launch.RequestPayload,
		&launch.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return LaunchRecord{}, false, nil
	}
	return launch, err == nil, err
}

func lockStudentLaunch(ctx context.Context, tx pgx.Tx, studentID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, studentID)
	return err
}

func findBlockingLabRunID(ctx context.Context, tx pgx.Tx, studentID string) (string, bool, error) {
	var labRunID string
	err := tx.QueryRow(ctx, `
SELECT l.lab_run_id::text
FROM lms_gateway.launches l
LEFT JOIN core.lab_runs lr ON lr.id = l.lab_run_id
WHERE l.local_student_id = $1
  AND (lr.id IS NULL OR lr.state NOT IN ('FINISHED', 'FAILED'))
ORDER BY l.created_at DESC
LIMIT 1`, studentID).Scan(&labRunID)
	if err == nil {
		return labRunID, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}

	err = tx.QueryRow(ctx, `
SELECT id::text
FROM core.lab_runs
WHERE student_id = $1
  AND state NOT IN ('FINISHED', 'FAILED')
ORDER BY updated_at DESC
LIMIT 1`, studentID).Scan(&labRunID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return labRunID, true, nil
}

func insertOutbox(ctx context.Context, tx pgx.Tx, envelope contracts.Envelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO lms_gateway.outbox (message_id, message_kind, subject, payload, status)
VALUES ($1, $2, $3, $4, 'PENDING')
ON CONFLICT (message_id) DO NOTHING`,
		envelope.MessageID, string(envelope.MessageKind), envelope.MessageType, payload)
	return err
}
