package outbox

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
)

type PostgresStore struct {
	db    *pgxpool.Pool
	table string
}

type Message struct {
	ID       int64              `json:"id"`
	Subject  string             `json:"subject"`
	Attempts int                `json:"attempts"`
	Envelope contracts.Envelope `json:"envelope"`
}

func NewPostgresStore(db *pgxpool.Pool, schema string) (*PostgresStore, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	if !isSafeIdentifier(schema) {
		return nil, fmt.Errorf("unsafe schema name %q", schema)
	}
	table := pgx.Identifier{schema, "outbox"}.Sanitize()
	return &PostgresStore{db: db, table: table}, nil
}

func (s *PostgresStore) Enqueue(ctx context.Context, envelope contracts.Envelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
INSERT INTO %s (message_id, message_kind, subject, payload, status)
VALUES ($1, $2, $3, $4, 'PENDING')
ON CONFLICT (message_id) DO NOTHING`, s.table)

	_, err = s.db.Exec(ctx, query, envelope.MessageID, string(envelope.MessageKind), envelope.MessageType, payload)
	return err
}

func (s *PostgresStore) FetchPending(ctx context.Context, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 1
	}

	query := fmt.Sprintf(`
WITH picked AS (
	SELECT id
	FROM %s
	WHERE status = 'PENDING' AND available_at <= now()
	ORDER BY created_at
	LIMIT $1
	FOR UPDATE SKIP LOCKED
)
UPDATE %s AS outbox
SET status = 'PROCESSING',
    attempts = attempts + 1,
    updated_at = now()
FROM picked
WHERE outbox.id = picked.id
RETURNING outbox.id, outbox.subject, outbox.attempts, outbox.payload`, s.table, s.table)

	rows, err := s.db.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := make([]Message, 0, limit)
	for rows.Next() {
		var message Message
		var payload []byte
		if err := rows.Scan(&message.ID, &message.Subject, &message.Attempts, &payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &message.Envelope); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (s *PostgresStore) MarkPublished(ctx context.Context, id int64) error {
	query := fmt.Sprintf(`
UPDATE %s
SET status = 'PUBLISHED',
    published_at = now(),
    last_error = NULL,
    updated_at = now()
WHERE id = $1`, s.table)
	_, err := s.db.Exec(ctx, query, id)
	return err
}

func (s *PostgresStore) MarkFailed(ctx context.Context, id int64, publishErr error, delay time.Duration) error {
	if delay < 0 {
		delay = 0
	}
	query := fmt.Sprintf(`
UPDATE %s
SET status = 'PENDING',
    available_at = now() + $2::interval,
    last_error = $3,
    updated_at = now()
WHERE id = $1`, s.table)
	_, err := s.db.Exec(ctx, query, id, pgInterval(delay), truncateError(publishErr))
	return err
}

func (s *PostgresStore) MarkDeadLetter(ctx context.Context, id int64, publishErr error) error {
	query := fmt.Sprintf(`
UPDATE %s
SET status = 'DLQ',
    last_error = $2,
    updated_at = now()
WHERE id = $1`, s.table)
	_, err := s.db.Exec(ctx, query, id, truncateError(publishErr))
	return err
}

func pgInterval(delay time.Duration) string {
	return fmt.Sprintf("%f seconds", delay.Seconds())
}

func truncateError(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) <= 1024 {
		return text
	}
	return text[:1024]
}

func isSafeIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if r == '_' || (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return !strings.HasPrefix(value, "_")
}
