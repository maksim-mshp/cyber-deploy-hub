package cloudadapter

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

func (r *PostgresRepository) SaveDeploySuccess(ctx context.Context, command contracts.Envelope, req DeployRequest, result DeployResult, secret EncryptedSecret, event contracts.Envelope) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		if err := upsertDeployment(ctx, tx, req, result, secret, deploymentStateDeployed, ""); err != nil {
			return err
		}
		if err := replaceInstances(ctx, tx, req.LabRunID, result.Instances); err != nil {
			return err
		}
		return insertOutbox(ctx, tx, event)
	})
}

func (r *PostgresRepository) SaveDeployFailure(ctx context.Context, command contracts.Envelope, req DeployRequest, result DeployResult, reason string, event contracts.Envelope) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		if err := upsertDeployment(ctx, tx, req, result, EncryptedSecret{}, deploymentStateFailed, reason); err != nil {
			return err
		}
		if err := replaceInstances(ctx, tx, req.LabRunID, result.Instances); err != nil {
			return err
		}
		return insertOutbox(ctx, tx, event)
	})
}

func (r *PostgresRepository) LoadDeployment(ctx context.Context, labRunID string) (Deployment, bool, error) {
	var deployment Deployment
	err := r.db.QueryRow(ctx, `
SELECT lab_run_id::text,
       project_id,
       state,
       COALESCE(keypair_name, ''),
       private_key_ciphertext,
       private_key_nonce,
       COALESCE(private_key_key_id, '')
FROM cloud_adapter.deployments
WHERE lab_run_id = $1`,
		labRunID).Scan(
		&deployment.LabRunID,
		&deployment.ProjectID,
		&deployment.State,
		&deployment.KeyPairName,
		&deployment.PrivateKeyCiphertext,
		&deployment.PrivateKeyNonce,
		&deployment.PrivateKeyKeyID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Deployment{}, false, nil
	}
	if err != nil {
		return Deployment{}, false, err
	}

	rows, err := r.db.Query(ctx, `
SELECT name,
       image_id,
       flavor_id,
       COALESCE(fixed_ip::text, ''),
       disk_gib,
       COALESCE(server_id, ''),
       COALESCE(volume_id, ''),
       COALESCE(port_id, ''),
       state
FROM cloud_adapter.instances
WHERE lab_run_id = $1
ORDER BY id`, labRunID)
	if err != nil {
		return Deployment{}, false, err
	}
	defer rows.Close()

	for rows.Next() {
		var instance Instance
		if err := rows.Scan(
			&instance.Name,
			&instance.ImageID,
			&instance.FlavorID,
			&instance.FixedIP,
			&instance.DiskGiB,
			&instance.ServerID,
			&instance.VolumeID,
			&instance.PortID,
			&instance.State,
		); err != nil {
			return Deployment{}, false, err
		}
		deployment.Instances = append(deployment.Instances, instance)
	}
	return deployment, true, rows.Err()
}

func (r *PostgresRepository) SaveCleanupSuccess(ctx context.Context, command contracts.Envelope, labRunID string, event contracts.Envelope) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
UPDATE cloud_adapter.deployments
SET state = $2,
    cleaned_at = now(),
    updated_at = now()
WHERE lab_run_id = $1`,
			labRunID, deploymentStateCleaned); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE cloud_adapter.instances
SET state = $2,
    updated_at = now()
WHERE lab_run_id = $1`,
			labRunID, deploymentStateCleaned); err != nil {
			return err
		}
		return insertOutbox(ctx, tx, event)
	})
}

func (r *PostgresRepository) SaveCleanupFailure(ctx context.Context, command contracts.Envelope, labRunID string, reason string, event contracts.Envelope) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
UPDATE cloud_adapter.deployments
SET failure_reason = NULLIF($2, ''),
    updated_at = now()
WHERE lab_run_id = $1`,
			labRunID, reason); err != nil {
			return err
		}
		return insertOutbox(ctx, tx, event)
	})
}

func upsertDeployment(ctx context.Context, tx pgx.Tx, req DeployRequest, result DeployResult, secret EncryptedSecret, state string, reason string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO cloud_adapter.deployments (
    lab_run_id,
    project_id,
    state,
    keypair_name,
    private_key_ciphertext,
    private_key_nonce,
    private_key_key_id,
    failure_reason
)
VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, NULLIF($7, ''), NULLIF($8, ''))
ON CONFLICT (lab_run_id) DO UPDATE
SET project_id = EXCLUDED.project_id,
    state = EXCLUDED.state,
    keypair_name = COALESCE(EXCLUDED.keypair_name, cloud_adapter.deployments.keypair_name),
    private_key_ciphertext = COALESCE(EXCLUDED.private_key_ciphertext, cloud_adapter.deployments.private_key_ciphertext),
    private_key_nonce = COALESCE(EXCLUDED.private_key_nonce, cloud_adapter.deployments.private_key_nonce),
    private_key_key_id = COALESCE(EXCLUDED.private_key_key_id, cloud_adapter.deployments.private_key_key_id),
    failure_reason = EXCLUDED.failure_reason,
    updated_at = now()`,
		req.LabRunID,
		req.ProjectID,
		state,
		result.KeyPairName,
		emptyBytesToNil(secret.Ciphertext),
		emptyBytesToNil(secret.Nonce),
		secret.KeyID,
		reason,
	)
	return err
}

func replaceInstances(ctx context.Context, tx pgx.Tx, labRunID string, instances []Instance) error {
	if _, err := tx.Exec(ctx, `DELETE FROM cloud_adapter.instances WHERE lab_run_id = $1`, labRunID); err != nil {
		return err
	}
	for _, instance := range instances {
		if _, err := tx.Exec(ctx, `
INSERT INTO cloud_adapter.instances (
    lab_run_id,
    name,
    image_id,
    flavor_id,
    fixed_ip,
    disk_gib,
    server_id,
    volume_id,
    port_id,
    state
)
VALUES ($1, $2, $3, $4, NULLIF($5, '')::inet, $6, NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), $10)`,
			labRunID,
			instance.Name,
			instance.ImageID,
			instance.FlavorID,
			instance.FixedIP,
			instance.DiskGiB,
			instance.ServerID,
			instance.VolumeID,
			instance.PortID,
			instance.State,
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
INSERT INTO cloud_adapter.outbox (message_id, message_kind, subject, payload, status)
VALUES ($1, $2, $3, $4, 'PENDING')
ON CONFLICT (message_id) DO NOTHING`,
		envelope.MessageID, string(envelope.MessageKind), envelope.MessageType, payload)
	return err
}

func emptyBytesToNil(value []byte) []byte {
	if len(value) == 0 {
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
