package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/events"
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

func (r *PostgresRepository) LoadSettings(ctx context.Context, defaults RuntimeSettings) (RuntimeSettings, error) {
	rows, err := r.db.Query(ctx, `SELECT key, value FROM lifecycle.settings`)
	if err != nil {
		return RuntimeSettings{}, err
	}
	defer rows.Close()

	overrides := map[string]any{}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return RuntimeSettings{}, err
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return RuntimeSettings{}, fmt.Errorf("decode setting %s: %w", key, err)
		}
		overrides[key] = value
	}
	if err := rows.Err(); err != nil {
		return RuntimeSettings{}, err
	}
	return MergeSettings(defaults, overrides)
}

func (r *PostgresRepository) ScheduleCleanup(ctx context.Context, command contracts.Envelope, timer Timer, event contracts.Envelope) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		timerID, err := upsertTimer(ctx, tx, timer)
		if err != nil {
			return err
		}
		if err := insertTimerHistory(ctx, tx, timerID, timer, command.MessageID); err != nil {
			return err
		}
		return insertOutbox(ctx, tx, event)
	})
}

func (r *PostgresRepository) FreezeLab(ctx context.Context, command contracts.Envelope, timer Timer, event contracts.Envelope) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		timerID, err := upsertTimer(ctx, tx, timer)
		if err != nil {
			return err
		}
		if err := insertTimerHistory(ctx, tx, timerID, timer, command.MessageID); err != nil {
			return err
		}
		return insertOutbox(ctx, tx, event)
	})
}

func (r *PostgresRepository) CancelCleanup(ctx context.Context, command contracts.Envelope, labRunID string, reason string) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
UPDATE lifecycle.timers
SET state = 'CANCELLED',
    reason = NULLIF($2, ''),
    updated_at = now()
WHERE lab_run_id = $1 AND kind = 'CLEANUP' AND state = 'SCHEDULED'
RETURNING id, lab_run_id::text, kind, state, due_at, COALESCE(reason, ''), COALESCE(created_by_message_id::text, '')`,
			labRunID, reason)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var timer Timer
			if err := rows.Scan(&timer.ID, &timer.LabRunID, &timer.Kind, &timer.State, &timer.DueAt, &timer.Reason, &timer.CreatedByMessageID); err != nil {
				return err
			}
			if err := insertTimerHistory(ctx, tx, timer.ID, timer, command.MessageID); err != nil {
				return err
			}
		}
		return rows.Err()
	})
}

func (r *PostgresRepository) SaveSettings(ctx context.Context, command contracts.Envelope, changedBy string, values map[string]any, event contracts.Envelope) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		for key, value := range values {
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO lifecycle.settings (key, value, updated_by, updated_at)
VALUES ($1, $2, NULLIF($3, ''), now())
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()`,
				key, raw, changedBy); err != nil {
				return err
			}
		}
		return insertOutbox(ctx, tx, event)
	})
}

func (r *PostgresRepository) FireDueTimers(ctx context.Context, now time.Time, limit int, producer string) (int, error) {
	if limit <= 0 {
		limit = 1
	}
	fired := 0
	err := r.tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
WITH picked AS (
    SELECT id
    FROM lifecycle.timers
    WHERE state = 'SCHEDULED' AND due_at <= $1
    ORDER BY due_at
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
UPDATE lifecycle.timers AS timers
SET state = 'FIRED',
    fired_at = $1,
    updated_at = now()
FROM picked
WHERE timers.id = picked.id
RETURNING timers.id,
          timers.lab_run_id::text,
          timers.kind,
          timers.state,
          timers.due_at,
          COALESCE(timers.reason, ''),
          COALESCE(timers.created_by_message_id::text, '')`,
			now, limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var timer Timer
			if err := rows.Scan(&timer.ID, &timer.LabRunID, &timer.Kind, &timer.State, &timer.DueAt, &timer.Reason, &timer.CreatedByMessageID); err != nil {
				return err
			}
			event, err := dueEvent(producer, timer)
			if err != nil {
				return err
			}
			if err := insertTimerHistory(ctx, tx, timer.ID, timer, event.MessageID); err != nil {
				return err
			}
			if err := insertOutbox(ctx, tx, event); err != nil {
				return err
			}
			fired++
		}
		return rows.Err()
	})
	return fired, err
}

func upsertTimer(ctx context.Context, tx pgx.Tx, timer Timer) (int64, error) {
	var timerID int64
	err := tx.QueryRow(ctx, `
INSERT INTO lifecycle.timers (
    lab_run_id,
    kind,
    state,
    due_at,
    reason,
    created_by_message_id,
    fired_at
)
VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, NULL)
ON CONFLICT (lab_run_id, kind) DO UPDATE
SET state = EXCLUDED.state,
    due_at = EXCLUDED.due_at,
    reason = EXCLUDED.reason,
    created_by_message_id = EXCLUDED.created_by_message_id,
    fired_at = NULL,
    updated_at = now()
RETURNING id`,
		timer.LabRunID,
		timer.Kind,
		timer.State,
		timer.DueAt,
		timer.Reason,
		uuidOrNil(timer.CreatedByMessageID),
	).Scan(&timerID)
	return timerID, err
}

func insertTimerHistory(ctx context.Context, tx pgx.Tx, timerID int64, timer Timer, messageID string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO lifecycle.timer_history (
    timer_id,
    lab_run_id,
    kind,
    state,
    due_at,
    reason,
    message_id
)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7)`,
		timerID,
		timer.LabRunID,
		timer.Kind,
		timer.State,
		timer.DueAt,
		timer.Reason,
		uuidOrNil(messageID),
	)
	return err
}

func dueEvent(producer string, timer Timer) (contracts.Envelope, error) {
	return contracts.NewEnvelope(contracts.NewEnvelopeParams{
		Kind:           contracts.MessageKindEvent,
		Type:           events.LifecycleCleanupDueV1,
		Producer:       producer,
		AggregateType:  aggregateTypeLabRun,
		AggregateID:    timer.LabRunID,
		IdempotencyKey: timer.LabRunID + ":" + events.LifecycleCleanupDueV1.String() + ":" + timer.DueAt.UTC().Format(time.RFC3339),
		Payload: events.LabRunEventPayload{
			LabRunID: timer.LabRunID,
			State:    "CLEANUP_DUE",
		},
	})
}

func insertOutbox(ctx context.Context, tx pgx.Tx, envelope contracts.Envelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO lifecycle.outbox (message_id, message_kind, subject, payload, status)
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
