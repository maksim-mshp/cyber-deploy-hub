package readmodel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"time"

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

func (r *PostgresReader) ListLabRuns(ctx context.Context, limit int) (LabRunsView, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
SELECT id::text,
       student_id,
       course_id,
       lab_id,
       state,
       COALESCE(failure_code, ''),
       COALESCE(failure_message, ''),
       created_at,
       updated_at,
       (
           SELECT due_at
           FROM lifecycle.timers
           WHERE lab_run_id = core.lab_runs.id
             AND kind = 'CLEANUP'
             AND state = 'SCHEDULED'
           LIMIT 1
       ) AS cleanup_due_at
FROM core.lab_runs
ORDER BY updated_at DESC
LIMIT $1`, limit)
	if err != nil {
		return LabRunsView{}, err
	}
	defer rows.Close()

	view := LabRunsView{Labs: []LabRunView{}}
	for rows.Next() {
		var item LabRunView
		var cleanupDueAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.StudentID,
			&item.CourseID,
			&item.LabID,
			&item.State,
			&item.FailureCode,
			&item.FailureMessage,
			&item.CreatedAt,
			&item.UpdatedAt,
			&cleanupDueAt,
		); err != nil {
			return LabRunsView{}, err
		}
		item.CleanupDueAt = nullTimePtr(cleanupDueAt)
		view.Labs = append(view.Labs, item)
	}
	return view, rows.Err()
}

func (r *PostgresReader) ListLabRunsByStudent(ctx context.Context, studentID string, limit int) (LabRunsView, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
SELECT id::text,
       student_id,
       course_id,
       lab_id,
       state,
       COALESCE(failure_code, ''),
       COALESCE(failure_message, ''),
       created_at,
       updated_at,
       (
           SELECT due_at
           FROM lifecycle.timers
           WHERE lab_run_id = core.lab_runs.id
             AND kind = 'CLEANUP'
             AND state = 'SCHEDULED'
           LIMIT 1
       ) AS cleanup_due_at
FROM core.lab_runs
WHERE student_id = $1
ORDER BY updated_at DESC
LIMIT $2`, studentID, limit)
	if err != nil {
		return LabRunsView{}, err
	}
	defer rows.Close()

	view := LabRunsView{Labs: []LabRunView{}}
	for rows.Next() {
		var item LabRunView
		var cleanupDueAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.StudentID,
			&item.CourseID,
			&item.LabID,
			&item.State,
			&item.FailureCode,
			&item.FailureMessage,
			&item.CreatedAt,
			&item.UpdatedAt,
			&cleanupDueAt,
		); err != nil {
			return LabRunsView{}, err
		}
		item.CleanupDueAt = nullTimePtr(cleanupDueAt)
		view.Labs = append(view.Labs, item)
	}
	return view, rows.Err()
}

func (r *PostgresReader) HasActiveLabRun(ctx context.Context, studentID string) (bool, error) {
	var active bool
	err := r.db.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM core.lab_runs
    WHERE student_id = $1
      AND state NOT IN ('FINISHED', 'FAILED')
)`, studentID).Scan(&active)
	return active, err
}

func (r *PostgresReader) GetLabRun(ctx context.Context, labRunID string) (LabRunView, bool, error) {
	var view LabRunView
	var cleanupDueAt sql.NullTime
	err := r.db.QueryRow(ctx, `
SELECT id::text,
       student_id,
       course_id,
       lab_id,
       state,
       COALESCE(failure_code, ''),
       COALESCE(failure_message, ''),
       created_at,
       updated_at,
       (
           SELECT due_at
           FROM lifecycle.timers
           WHERE lab_run_id = core.lab_runs.id
             AND kind = 'CLEANUP'
             AND state = 'SCHEDULED'
           LIMIT 1
       ) AS cleanup_due_at
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
		&cleanupDueAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return LabRunView{}, false, nil
	}
	if err != nil {
		return LabRunView{}, false, err
	}
	view.CleanupDueAt = nullTimePtr(cleanupDueAt)
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

func (r *PostgresReader) ListLabInstances(ctx context.Context, labRunID string) (LabInstancesView, bool, error) {
	vdi, found, err := r.GetVDIAccess(ctx, labRunID)
	if err != nil || !found {
		return LabInstancesView{}, found, err
	}

	rows, err := r.db.Query(ctx, `
SELECT name,
       state,
       COALESCE(server_id, ''),
       COALESCE(volume_id, ''),
       COALESCE(port_id, ''),
       COALESCE(host(fixed_ip), ''),
       COALESCE(host(access_ip), ''),
       image_id,
       flavor_id,
       disk_gib
FROM cloud_adapter.instances
WHERE lab_run_id = $1
ORDER BY id`, labRunID)
	if err != nil {
		return LabInstancesView{}, false, err
	}
	defer rows.Close()

	view := LabInstancesView{LabRunID: labRunID, Instances: []LabInstanceView{}}
	for rows.Next() {
		var item LabInstanceView
		if err := rows.Scan(
			&item.Name,
			&item.State,
			&item.ServerID,
			&item.VolumeID,
			&item.PortID,
			&item.FixedIP,
			&item.AccessIP,
			&item.ImageID,
			&item.FlavorID,
			&item.DiskGiB,
		); err != nil {
			return LabInstancesView{}, false, err
		}
		item.VDIAccess = instanceVDIAccess(vdi, item)
		view.Instances = append(view.Instances, item)
	}
	return view, true, rows.Err()
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
		events[i].Payload = publicEventPayload(events[i].MessageType, events[i].Payload)
	}
	return events, nil
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	next := value.Time
	return &next
}

func instanceVDIAccess(base VDIAccessView, instance LabInstanceView) VDIAccessView {
	access := VDIAccessView{
		LabRunID:  base.LabRunID,
		Available: base.Available && instance.State == "ACTIVE" && instance.ServerID != "",
		State:     base.State,
		Reason:    base.Reason,
	}
	if !access.Available {
		if access.Reason == "" {
			access.Reason = "instance_vdi_unavailable"
		}
		return access
	}
	instanceURL, err := appendVDITarget(base.URL, instance)
	if err != nil {
		access.Available = false
		access.Reason = "invalid_vdi_url"
		return access
	}
	access.URL = instanceURL
	return access
}

func appendVDITarget(rawURL string, instance LabInstanceView) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("server_id", instance.ServerID)
	query.Set("instance_name", instance.Name)
	if instance.FixedIP != "" {
		query.Set("fixed_ip", instance.FixedIP)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func publicEventPayload(messageType string, payload json.RawMessage) json.RawMessage {
	switch messageType {
	case "evt.capacity.approved.v1", "evt.capacity.denied.v1":
		var envelope struct {
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(payload, &envelope); err == nil && len(envelope.Payload) > 0 {
			return envelope.Payload
		}
		return payload
	default:
		return nil
	}
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
SELECT p.id::text,
       p.name,
       p.domain_id,
       d.course_id,
       p.state,
       COALESCE(p.current_lab_run_id::text, ''),
       COALESCE(p.reserved_by_student_id, '')
FROM project_pool.ki_projects AS p
JOIN project_pool.domains AS d ON d.domain_id = p.domain_id
ORDER BY d.course_id, p.domain_id, p.name`)
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
			&item.CourseID,
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

func (r *PostgresReader) ListCheckRuns(ctx context.Context, labRunID string, limit int) (CheckRunsView, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := r.db.Query(ctx, `
SELECT id::text,
       lab_run_id::text,
       profile_id,
       state,
       COALESCE(passed, false),
       COALESCE(error_code, ''),
       COALESCE(error_message, ''),
       started_at,
       finished_at
FROM checker.runs
WHERE lab_run_id = $1
ORDER BY finished_at DESC
LIMIT $2`, labRunID, limit)
	if err != nil {
		return CheckRunsView{}, err
	}
	defer rows.Close()

	view := CheckRunsView{}
	for rows.Next() {
		var run CheckRunView
		if err := rows.Scan(
			&run.ID,
			&run.LabRunID,
			&run.ProfileID,
			&run.State,
			&run.Passed,
			&run.ErrorCode,
			&run.ErrorMessage,
			&run.StartedAt,
			&run.FinishedAt,
		); err != nil {
			return CheckRunsView{}, err
		}
		results, err := r.listCheckStepResults(ctx, run.ID)
		if err != nil {
			return CheckRunsView{}, err
		}
		run.Results = results
		view.Runs = append(view.Runs, run)
	}
	return view, rows.Err()
}

func (r *PostgresReader) listCheckStepResults(ctx context.Context, runID string) ([]CheckStepResultView, error) {
	rows, err := r.db.Query(ctx, `
SELECT sequence,
       name,
       type,
       passed,
       exit_code,
       COALESCE(message, ''),
       COALESCE(stdout_tail, ''),
       COALESCE(stderr_tail, ''),
       started_at,
       finished_at
FROM checker.step_results
WHERE run_id = $1
ORDER BY sequence`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []CheckStepResultView{}
	for rows.Next() {
		var result CheckStepResultView
		if err := rows.Scan(
			&result.Sequence,
			&result.Name,
			&result.Type,
			&result.Passed,
			&result.ExitCode,
			&result.Message,
			&result.StdoutTail,
			&result.StderrTail,
			&result.StartedAt,
			&result.FinishedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
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
