package checker

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

func (r *PostgresRepository) SeedProfiles(ctx context.Context, profiles []Profile) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		for _, profile := range profiles {
			if err := validateProfile(profile); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO checker.profiles (profile_id, name, ssh_user)
VALUES ($1, $2, $3)
ON CONFLICT (profile_id) DO UPDATE
SET name = EXCLUDED.name,
    ssh_user = EXCLUDED.ssh_user,
    updated_at = now()`,
				profile.ID, profile.Name, profile.SSHUser); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM checker.profile_steps WHERE profile_id = $1`, profile.ID); err != nil {
				return err
			}
			for _, step := range profile.Steps {
				if _, err := tx.Exec(ctx, `
INSERT INTO checker.profile_steps (
    profile_id,
    sequence,
    name,
    type,
    package_name,
    path,
    contains,
    service_name,
    port,
    command,
    expected_exit_code,
    timeout_seconds
)
VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, 0), NULLIF($10, ''), $11, $12)`,
					profile.ID,
					step.Sequence,
					step.Name,
					string(step.Type),
					step.Package,
					step.Path,
					step.Contains,
					step.Service,
					step.Port,
					step.Command,
					step.ExpectedExitCode,
					step.TimeoutSeconds,
				); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (r *PostgresRepository) LoadProfile(ctx context.Context, profileID string) (Profile, bool, error) {
	var profile Profile
	err := r.db.QueryRow(ctx, `
SELECT profile_id, name, ssh_user
FROM checker.profiles
WHERE profile_id = $1`, profileID).Scan(&profile.ID, &profile.Name, &profile.SSHUser)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, false, nil
	}
	if err != nil {
		return Profile{}, false, err
	}

	rows, err := r.db.Query(ctx, `
SELECT sequence,
       name,
       type,
       COALESCE(package_name, ''),
       COALESCE(path, ''),
       COALESCE(contains, ''),
       COALESCE(service_name, ''),
       COALESCE(port, 0),
       COALESCE(command, ''),
       expected_exit_code,
       timeout_seconds
FROM checker.profile_steps
WHERE profile_id = $1
ORDER BY sequence`, profileID)
	if err != nil {
		return Profile{}, false, err
	}
	defer rows.Close()

	for rows.Next() {
		var step Step
		var stepType string
		if err := rows.Scan(
			&step.Sequence,
			&step.Name,
			&stepType,
			&step.Package,
			&step.Path,
			&step.Contains,
			&step.Service,
			&step.Port,
			&step.Command,
			&step.ExpectedExitCode,
			&step.TimeoutSeconds,
		); err != nil {
			return Profile{}, false, err
		}
		step.Type = StepType(stepType)
		profile.Steps = append(profile.Steps, step)
	}
	if err := rows.Err(); err != nil {
		return Profile{}, false, err
	}
	if err := validateProfile(profile); err != nil {
		return Profile{}, false, err
	}
	return profile, true, nil
}

func (r *PostgresRepository) LoadTarget(ctx context.Context, labRunID string, defaultPort int) (Target, bool, error) {
	var target Target
	err := r.db.QueryRow(ctx, `
SELECT d.lab_run_id::text,
       d.project_id,
       COALESCE(i.fixed_ip, ''),
       d.private_key_ciphertext,
       d.private_key_nonce,
       COALESCE(d.private_key_key_id, '')
FROM cloud_adapter.deployments d
LEFT JOIN LATERAL (
    SELECT host(COALESCE(access_ip, fixed_ip)) AS fixed_ip
    FROM cloud_adapter.instances
    WHERE lab_run_id = d.lab_run_id AND COALESCE(access_ip, fixed_ip) IS NOT NULL
    ORDER BY id
    LIMIT 1
) i ON true
WHERE d.lab_run_id = $1`,
		labRunID).Scan(
		&target.LabRunID,
		&target.ProjectID,
		&target.Host,
		&target.EncryptedPrivateKey,
		&target.PrivateKeyNonce,
		&target.PrivateKeyKeyID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, false, nil
	}
	if err != nil {
		return Target{}, false, err
	}
	target.Port = defaultPort
	return target, true, nil
}

func (r *PostgresRepository) SaveCompleted(ctx context.Context, command contracts.Envelope, run RunRecord, event contracts.Envelope) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		if err := upsertRun(ctx, tx, run); err != nil {
			return err
		}
		if err := replaceStepResults(ctx, tx, run.ID, run.Results); err != nil {
			return err
		}
		return insertOutbox(ctx, tx, event)
	})
}

func (r *PostgresRepository) SaveFailed(ctx context.Context, command contracts.Envelope, run RunRecord, event contracts.Envelope) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		if err := upsertRun(ctx, tx, run); err != nil {
			return err
		}
		if err := replaceStepResults(ctx, tx, run.ID, run.Results); err != nil {
			return err
		}
		return insertOutbox(ctx, tx, event)
	})
}

func upsertRun(ctx context.Context, tx pgx.Tx, run RunRecord) error {
	_, err := tx.Exec(ctx, `
INSERT INTO checker.runs (
    id,
    lab_run_id,
    profile_id,
    state,
    passed,
    error_code,
    error_message,
    started_at,
    finished_at
)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, $9)
ON CONFLICT (id) DO UPDATE
SET state = EXCLUDED.state,
    passed = EXCLUDED.passed,
    error_code = EXCLUDED.error_code,
    error_message = EXCLUDED.error_message,
    finished_at = EXCLUDED.finished_at`,
		run.ID,
		run.LabRunID,
		run.ProfileID,
		run.State,
		run.Passed,
		run.ErrorCode,
		run.ErrorMessage,
		run.StartedAt,
		run.FinishedAt,
	)
	return err
}

func replaceStepResults(ctx context.Context, tx pgx.Tx, runID string, results []StepResult) error {
	if _, err := tx.Exec(ctx, `DELETE FROM checker.step_results WHERE run_id = $1`, runID); err != nil {
		return err
	}
	for _, result := range results {
		if _, err := tx.Exec(ctx, `
INSERT INTO checker.step_results (
    run_id,
    sequence,
    name,
    type,
    passed,
    exit_code,
    message,
    stdout_tail,
    stderr_tail,
    started_at,
    finished_at
)
VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), $10, $11)`,
			runID,
			result.Sequence,
			result.Name,
			string(result.Type),
			result.Passed,
			result.ExitCode,
			result.Message,
			result.StdoutTail,
			result.StderrTail,
			result.StartedAt,
			result.FinishedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func insertOutbox(ctx context.Context, tx pgx.Tx, envelope contracts.Envelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO checker.outbox (message_id, message_kind, subject, payload, status)
VALUES ($1, $2, $3, $4, 'PENDING')
ON CONFLICT (message_id) DO NOTHING`,
		envelope.MessageID, string(envelope.MessageKind), envelope.MessageType, payload)
	return err
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
