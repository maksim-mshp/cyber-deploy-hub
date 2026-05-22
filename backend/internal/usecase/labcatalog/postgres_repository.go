package labcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/contracts/commands"
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

func (r *PostgresRepository) List(ctx context.Context, includeDisabled bool) ([]Definition, error) {
	rows, err := r.db.Query(ctx, `
SELECT course_id,
       lab_id,
       title,
       description,
       enabled,
       resources,
       instances,
       check_profile,
       COALESCE(updated_by, ''),
       created_at,
       updated_at
FROM core.lab_definitions
WHERE $1 OR enabled = true
ORDER BY course_id, lab_id`, includeDisabled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	labs := []Definition{}
	for rows.Next() {
		lab, err := scanDefinition(rows)
		if err != nil {
			return nil, err
		}
		labs = append(labs, lab)
	}
	return labs, rows.Err()
}

func (r *PostgresRepository) Get(ctx context.Context, courseID string, labID string) (Definition, bool, error) {
	row := r.db.QueryRow(ctx, `
SELECT course_id,
       lab_id,
       title,
       description,
       enabled,
       resources,
       instances,
       check_profile,
       COALESCE(updated_by, ''),
       created_at,
       updated_at
FROM core.lab_definitions
WHERE course_id = $1 AND lab_id = $2`, courseID, labID)
	lab, err := scanDefinition(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Definition{}, false, nil
	}
	if err != nil {
		return Definition{}, false, err
	}
	return lab, true, nil
}

func (r *PostgresRepository) Upsert(ctx context.Context, definition Definition, changedBy string) (Definition, error) {
	resources, err := json.Marshal(definition.Resources)
	if err != nil {
		return Definition{}, err
	}
	instances, err := json.Marshal(definition.Instances)
	if err != nil {
		return Definition{}, err
	}
	checkProfile, err := json.Marshal(definition.CheckProfile)
	if err != nil {
		return Definition{}, err
	}
	row := r.db.QueryRow(ctx, `
INSERT INTO core.lab_definitions (
    course_id,
    lab_id,
    title,
    description,
    enabled,
    resources,
    instances,
    check_profile,
    updated_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE(NULLIF($8::jsonb, 'null'::jsonb), '{}'::jsonb), $9)
ON CONFLICT (course_id, lab_id) DO UPDATE
SET title = EXCLUDED.title,
    description = EXCLUDED.description,
    enabled = EXCLUDED.enabled,
    resources = EXCLUDED.resources,
    instances = EXCLUDED.instances,
    check_profile = EXCLUDED.check_profile,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING course_id,
          lab_id,
          title,
          description,
          enabled,
          resources,
          instances,
          check_profile,
          COALESCE(updated_by, ''),
          created_at,
          updated_at`,
		definition.CourseID,
		definition.LabID,
		definition.Title,
		definition.Description,
		definition.Enabled,
		resources,
		instances,
		checkProfile,
		changedBy,
	)
	return scanDefinition(row)
}

type definitionScanner interface {
	Scan(dest ...any) error
}

func scanDefinition(row definitionScanner) (Definition, error) {
	var lab Definition
	var rawResources []byte
	var rawInstances []byte
	var rawCheckProfile []byte
	if err := row.Scan(
		&lab.CourseID,
		&lab.LabID,
		&lab.Title,
		&lab.Description,
		&lab.Enabled,
		&rawResources,
		&rawInstances,
		&rawCheckProfile,
		&lab.UpdatedBy,
		&lab.CreatedAt,
		&lab.UpdatedAt,
	); err != nil {
		return Definition{}, err
	}
	if err := json.Unmarshal(rawResources, &lab.Resources); err != nil {
		return Definition{}, fmt.Errorf("decode lab resources: %w", err)
	}
	if err := json.Unmarshal(rawInstances, &lab.Instances); err != nil {
		return Definition{}, fmt.Errorf("decode lab instances: %w", err)
	}
	if err := decodeCheckProfile(rawCheckProfile, &lab.CheckProfile); err != nil {
		return Definition{}, err
	}
	return lab, nil
}

func decodeCheckProfile(raw []byte, dst **commands.CheckerProfileV1) error {
	var profile commands.CheckerProfileV1
	if err := json.Unmarshal(raw, &profile); err != nil {
		return fmt.Errorf("decode lab check profile: %w", err)
	}
	if profile.ID == "" && profile.Name == "" && profile.SSHUser == "" && len(profile.Steps) == 0 {
		return nil
	}
	*dst = &profile
	return nil
}
