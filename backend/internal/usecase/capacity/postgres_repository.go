package capacity

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"cyber-deploy-hub/internal/contracts"
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

func (r *PostgresRepository) SaveCheck(ctx context.Context, command contracts.Envelope, req commands.CapacityCheckV1Payload, snapshot Snapshot, decision Decision, next []contracts.Envelope) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		snapshotID, err := insertSnapshot(ctx, tx, snapshot)
		if err != nil {
			return err
		}
		if err := insertDecision(ctx, tx, command, req, snapshotID, decision); err != nil {
			return err
		}
		if decision.Approved {
			if err := upsertReservation(ctx, tx, req); err != nil {
				return err
			}
		} else {
			if err := releaseReservationByLabRun(ctx, tx, req.LabRunID, decision.Reason); err != nil {
				return err
			}
		}
		for _, envelope := range next {
			if err := insertOutbox(ctx, tx, envelope); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *PostgresRepository) ReleaseReservation(ctx context.Context, command contracts.Envelope, req commands.LabRunCommandPayload, next contracts.Envelope) error {
	return r.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
UPDATE capacity.reservations
SET state = $2,
    released_at = now(),
    release_reason = NULLIF($3, ''),
    updated_at = now()
WHERE lab_run_id = $1`,
			req.LabRunID, reservationStateReleased, req.Reason); err != nil {
			return err
		}
		return insertOutbox(ctx, tx, next)
	})
}

func insertSnapshot(ctx context.Context, tx pgx.Tx, snapshot Snapshot) (int64, error) {
	raw := snapshot.RawPayload
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var id int64
	err := tx.QueryRow(ctx, `
INSERT INTO capacity.snapshots (
    source,
    vcpus_total,
    vcpus_free,
    ram_mib_total,
    ram_mib_free,
    storage_gib_total,
    storage_gib_used,
    raw_payload
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id`,
		snapshot.Source,
		snapshot.VCPUsTotal,
		snapshot.VCPUsFree,
		snapshot.RAMMiBTotal,
		snapshot.RAMMiBFree,
		snapshot.StorageGiBTotal,
		snapshot.StorageGiBUsed,
		raw,
	).Scan(&id)
	return id, err
}

func insertDecision(ctx context.Context, tx pgx.Tx, command contracts.Envelope, req commands.CapacityCheckV1Payload, snapshotID int64, decision Decision) error {
	_, err := tx.Exec(ctx, `
INSERT INTO capacity.decisions (
    lab_run_id,
    project_id,
    message_id,
    snapshot_id,
    approved,
    requested_vcpu,
    requested_ram_mib,
    requested_disk_gib,
    predicted_cpu_percent,
    predicted_ram_percent,
    predicted_storage_percent,
    threshold_percent,
    reason
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NULLIF($13, ''))
ON CONFLICT (message_id) DO NOTHING`,
		req.LabRunID,
		req.ProjectID,
		command.MessageID,
		snapshotID,
		decision.Approved,
		req.Resources.VCPU,
		req.Resources.RAMMiB,
		req.Resources.DiskGiB,
		decision.PredictedCPU,
		decision.PredictedRAM,
		decision.PredictedStorage,
		decision.Threshold,
		decision.Reason,
	)
	return err
}

func upsertReservation(ctx context.Context, tx pgx.Tx, req commands.CapacityCheckV1Payload) error {
	_, err := tx.Exec(ctx, `
INSERT INTO capacity.reservations (lab_run_id, project_id, vcpu, ram_mib, disk_gib, state)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (lab_run_id) DO UPDATE
SET project_id = EXCLUDED.project_id,
    vcpu = EXCLUDED.vcpu,
    ram_mib = EXCLUDED.ram_mib,
    disk_gib = EXCLUDED.disk_gib,
    state = EXCLUDED.state,
    released_at = NULL,
    release_reason = NULL,
    updated_at = now()`,
		req.LabRunID,
		req.ProjectID,
		req.Resources.VCPU,
		req.Resources.RAMMiB,
		req.Resources.DiskGiB,
		reservationStateActive,
	)
	return err
}

func releaseReservationByLabRun(ctx context.Context, tx pgx.Tx, labRunID string, reason string) error {
	_, err := tx.Exec(ctx, `
UPDATE capacity.reservations
SET state = $2,
    released_at = now(),
    release_reason = NULLIF($3, ''),
    updated_at = now()
WHERE lab_run_id = $1
  AND state = 'ACTIVE'`,
		labRunID, reservationStateReleased, reason)
	return err
}

func insertOutbox(ctx context.Context, tx pgx.Tx, envelope contracts.Envelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO capacity.outbox (message_id, message_kind, subject, payload, status)
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
