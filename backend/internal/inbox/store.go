package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/contracts"
)

type PostgresStore struct {
	db    *pgxpool.Pool
	table string
}

func NewPostgresStore(db *pgxpool.Pool, schema string) (*PostgresStore, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	if !isSafeIdentifier(schema) {
		return nil, fmt.Errorf("unsafe schema name %q", schema)
	}
	table := pgx.Identifier{schema, "inbox"}.Sanitize()
	return &PostgresStore{db: db, table: table}, nil
}

func (s *PostgresStore) Register(ctx context.Context, envelope contracts.Envelope) (bool, error) {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return false, err
	}

	query := fmt.Sprintf(`
INSERT INTO %s (message_id, message_kind, subject, payload, status)
VALUES ($1, $2, $3, $4, 'RECEIVED')
ON CONFLICT (message_id) DO NOTHING`, s.table)

	tag, err := s.db.Exec(ctx, query, envelope.MessageID, string(envelope.MessageKind), envelope.MessageType, payload)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *PostgresStore) MarkProcessed(ctx context.Context, messageID string) error {
	query := fmt.Sprintf(`
UPDATE %s
SET status = 'PROCESSED',
    processed_at = now(),
    updated_at = now()
WHERE message_id = $1`, s.table)
	_, err := s.db.Exec(ctx, query, messageID)
	return err
}

func (s *PostgresStore) MarkFailed(ctx context.Context, messageID string, handleErr error) error {
	query := fmt.Sprintf(`
UPDATE %s
SET status = 'FAILED',
    attempts = attempts + 1,
    last_error = $2,
    updated_at = now()
WHERE message_id = $1`, s.table)
	_, err := s.db.Exec(ctx, query, messageID, truncateError(handleErr))
	return err
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
