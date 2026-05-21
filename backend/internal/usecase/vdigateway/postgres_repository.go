package vdigateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

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

func (r *PostgresRepository) SaveIssued(ctx context.Context, command contracts.Envelope, token AccessToken, event contracts.Envelope) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
UPDATE vdi_gateway.access_tokens
SET state = 'REVOKED',
    revoked_at = now(),
    revoke_reason = 'superseded',
    updated_at = now()
WHERE lab_run_id = $1 AND state = 'ACTIVE'`,
			token.LabRunID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO vdi_gateway.access_tokens (
    token_hash,
    lab_run_id,
    student_id,
    project_id,
    state,
    expires_at,
    issued_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			token.TokenHash,
			token.LabRunID,
			token.StudentID,
			token.ProjectID,
			token.State,
			token.ExpiresAt,
			token.IssuedAt,
		); err != nil {
			return err
		}
		return insertOutbox(ctx, tx, event)
	})
}

func (r *PostgresRepository) SaveIssueFailure(ctx context.Context, command contracts.Envelope, labRunID string, studentID string, projectID string, reason string, event contracts.Envelope) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
INSERT INTO vdi_gateway.access_failures (
    lab_run_id,
    student_id,
    project_id,
    reason,
    message_id
)
VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4, $5)
ON CONFLICT (message_id) DO NOTHING`,
			uuidOrNil(labRunID),
			studentID,
			projectID,
			reason,
			event.MessageID,
		); err != nil {
			return err
		}
		return insertOutbox(ctx, tx, event)
	})
}

func (r *PostgresRepository) RevokeByLabRun(ctx context.Context, command contracts.Envelope, labRunID string, reason string, event contracts.Envelope) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
UPDATE vdi_gateway.access_tokens
SET state = 'REVOKED',
    revoked_at = now(),
    revoke_reason = NULLIF($2, ''),
    updated_at = now()
WHERE lab_run_id = $1 AND state = 'ACTIVE'`,
			labRunID, reason); err != nil {
			return err
		}
		return insertOutbox(ctx, tx, event)
	})
}

func (r *PostgresRepository) FindToken(ctx context.Context, tokenHash string) (AccessToken, bool, error) {
	var token AccessToken
	err := r.db.QueryRow(ctx, `
SELECT token_hash,
       lab_run_id::text,
       student_id,
       project_id,
       state,
       expires_at,
       issued_at,
       COALESCE(revoke_reason, '')
FROM vdi_gateway.access_tokens
WHERE token_hash = $1`,
		tokenHash).Scan(
		&token.TokenHash,
		&token.LabRunID,
		&token.StudentID,
		&token.ProjectID,
		&token.State,
		&token.ExpiresAt,
		&token.IssuedAt,
		&token.RevokeReason,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessToken{}, false, nil
	}
	if err != nil {
		return AccessToken{}, false, err
	}
	return token, true, nil
}

func (r *PostgresRepository) FindInstanceTarget(ctx context.Context, labRunID string, serverID string) (InstanceTarget, bool, error) {
	var target InstanceTarget
	err := r.db.QueryRow(ctx, `
SELECT COALESCE(server_id, ''),
       name,
       state
FROM cloud_adapter.instances
WHERE lab_run_id = $1 AND server_id = $2`,
		labRunID, serverID).Scan(&target.ServerID, &target.Name, &target.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return InstanceTarget{}, false, nil
	}
	if err != nil {
		return InstanceTarget{}, false, err
	}
	return target, true, nil
}

func (r *PostgresRepository) MarkExpired(ctx context.Context, tokenHash string) error {
	_, err := r.db.Exec(ctx, `
UPDATE vdi_gateway.access_tokens
SET state = 'EXPIRED',
    updated_at = now()
WHERE token_hash = $1 AND state = 'ACTIVE'`, tokenHash)
	return err
}

func (r *PostgresRepository) RecordOpen(ctx context.Context, tokenHash string, labRunID string, openedAt time.Time, remoteAddr string, userAgent string) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
UPDATE vdi_gateway.access_tokens
SET last_opened_at = $2,
    updated_at = now()
WHERE token_hash = $1`,
			tokenHash, openedAt); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
INSERT INTO vdi_gateway.session_opens (
    token_hash,
    lab_run_id,
    opened_at,
    remote_addr,
    user_agent
)
VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''))`,
			tokenHash, labRunID, openedAt, remoteAddr, userAgent)
		return err
	})
}

func insertOutbox(ctx context.Context, tx pgx.Tx, envelope contracts.Envelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO vdi_gateway.outbox (message_id, message_kind, subject, payload, status)
VALUES ($1, $2, $3, $4, 'PENDING')
ON CONFLICT (message_id) DO NOTHING`,
		envelope.MessageID, string(envelope.MessageKind), envelope.MessageType, payload)
	return err
}

func uuidOrNil(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
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
