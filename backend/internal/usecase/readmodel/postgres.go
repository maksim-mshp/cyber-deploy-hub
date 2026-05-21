package readmodel

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresReader struct {
	db *pgxpool.Pool
}

func NewPostgresReader(db *pgxpool.Pool) (*PostgresReader, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	return &PostgresReader{db: db}, nil
}

func (r *PostgresReader) GetLabRun(ctx context.Context, labRunID string) (LabRunView, bool, error) {
	var view LabRunView
	err := r.db.QueryRow(ctx, `
SELECT id::text,
       student_id,
       course_id,
       lab_id,
       state,
       COALESCE(failure_code, ''),
       COALESCE(failure_message, ''),
       created_at,
       updated_at
FROM core.lab_runs
WHERE id = $1`, labRunID).Scan(
		&view.ID,
		&view.StudentID,
		&view.CourseID,
		&view.LabID,
		&view.State,
		&view.FailureCode,
		&view.FailureMessage,
		&view.CreatedAt,
		&view.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return LabRunView{}, false, nil
	}
	if err != nil {
		return LabRunView{}, false, err
	}
	events, err := r.ListLabRunEvents(ctx, labRunID, 0, 50)
	if err != nil {
		return LabRunView{}, false, err
	}
	view.Events = events
	return view, true, nil
}

func (r *PostgresReader) GetVDIAccess(ctx context.Context, labRunID string) (VDIAccessView, bool, error) {
	var view VDIAccessView
	err := r.db.QueryRow(ctx, `
SELECT id::text,
       state,
       COALESCE(vdi_access_url, '')
FROM core.lab_runs
WHERE id = $1`, labRunID).Scan(&view.LabRunID, &view.State, &view.URL)
	if errors.Is(err, pgx.ErrNoRows) {
		return VDIAccessView{}, false, nil
	}
	if err != nil {
		return VDIAccessView{}, false, err
	}
	view.Available = view.URL != "" && (view.State == "READY" || view.State == "VERIFIED" || view.State == "VERIFICATION_FAILED")
	if !view.Available && view.URL == "" {
		view.Reason = "vdi_access_not_issued"
	}
	return view, true, nil
}

func (r *PostgresReader) ListLabRunEvents(ctx context.Context, labRunID string, afterID int64, limit int) ([]LabRunEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
SELECT id,
       lab_run_id::text,
       state,
       message_type,
       payload,
       created_at
FROM core.lab_run_events
WHERE lab_run_id = $1 AND id > $2
ORDER BY id
LIMIT $3`, labRunID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events, err := scanEvents(rows)
	if err != nil {
		return nil, err
	}
	for i := range events {
		events[i].Payload = nil
	}
	return events, nil
}

func (r *PostgresReader) ListAuditEvents(ctx context.Context, limit int) (AuditView, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.Query(ctx, `
SELECT id,
       lab_run_id::text,
       state,
       message_type,
       payload,
       created_at
FROM core.lab_run_events
ORDER BY id DESC
LIMIT $1`, limit)
	if err != nil {
		return AuditView{}, err
	}
	defer rows.Close()
	events, err := scanEvents(rows)
	if err != nil {
		return AuditView{}, err
	}
	return AuditView{Events: events}, nil
}

func (r *PostgresReader) GetSettings(ctx context.Context) (SettingsView, error) {
	rows, err := r.db.Query(ctx, `SELECT key, value FROM lifecycle.settings ORDER BY key`)
	if err != nil {
		return SettingsView{}, err
	}
	defer rows.Close()

	values := map[string]any{}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return SettingsView{}, err
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return SettingsView{}, err
		}
		values[key] = value
	}
	return SettingsView{Values: values}, rows.Err()
}

func (r *PostgresReader) GetProjectPool(ctx context.Context) (ProjectPoolView, error) {
	rows, err := r.db.Query(ctx, `
SELECT id::text,
       name,
       domain_id,
       state,
       COALESCE(current_lab_run_id::text, ''),
       COALESCE(reserved_by_student_id, '')
FROM project_pool.ki_projects
ORDER BY domain_id, name`)
	if err != nil {
		return ProjectPoolView{}, err
	}
	defer rows.Close()

	view := ProjectPoolView{States: map[string]int{}}
	for rows.Next() {
		var item ProjectPoolItem
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.DomainID,
			&item.State,
			&item.CurrentLabRunID,
			&item.ReservedByStudentID,
		); err != nil {
			return ProjectPoolView{}, err
		}
		view.States[item.State]++
		view.Projects = append(view.Projects, item)
	}
	return view, rows.Err()
}

func scanEvents(rows pgx.Rows) ([]LabRunEvent, error) {
	events := []LabRunEvent{}
	for rows.Next() {
		var event LabRunEvent
		if err := rows.Scan(
			&event.ID,
			&event.LabRunID,
			&event.State,
			&event.MessageType,
			&event.Payload,
			&event.CreatedAt,
		); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
