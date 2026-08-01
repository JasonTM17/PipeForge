package dataset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/upload"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) ReserveVersion(ctx context.Context, reservation VersionReservation) (DatasetVersion, error) {
	metadata, err := json.Marshal(reservation.SourceInfo)
	if err != nil {
		return DatasetVersion{}, fmt.Errorf("marshal source metadata: %w", err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return DatasetVersion{}, fmt.Errorf("begin version reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var datasetState string
	if err := tx.QueryRow(ctx, `SELECT state FROM datasets WHERE id = $1 FOR UPDATE`, reservation.DatasetID).Scan(&datasetState); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DatasetVersion{}, ErrDatasetNotFound
		}
		return DatasetVersion{}, fmt.Errorf("lock dataset for version: %w", err)
	}
	if datasetState == "DELETED" {
		return DatasetVersion{}, ErrDatasetDeleted
	}
	var versionNumber int32
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_number), 0) + 1 FROM dataset_versions WHERE dataset_id = $1`, reservation.DatasetID).Scan(&versionNumber); err != nil {
		return DatasetVersion{}, fmt.Errorf("allocate dataset version: %w", err)
	}
	var version DatasetVersion
	err = tx.QueryRow(ctx, `
INSERT INTO dataset_versions (id, dataset_id, version_number, original_filename, content_type, format, object_key, artifact_prefix, source_info, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, dataset_id, version_number, state, original_filename, content_type, format, size_bytes, object_key, checksum_sha256,
          row_count, column_count, encoding, compression_type, source_info, artifact_prefix, created_by, created_at, available_at`,
		reservation.ID, reservation.DatasetID, versionNumber, reservation.OriginalFilename, reservation.ContentType, string(reservation.Format), reservation.ObjectKey, reservation.ArtifactPrefix, metadata, reservation.CreatedBy).
		Scan(&version.ID, &version.DatasetID, &version.VersionNumber, &version.State, &version.OriginalFilename, &version.ContentType, &version.Format, &version.SizeBytes, &version.ObjectKey, &version.ChecksumSHA256, &version.RowCount, &version.ColumnCount, &version.Encoding, &version.CompressionType, &metadata, &version.ArtifactPrefix, &version.CreatedBy, &version.CreatedAt, &version.AvailableAt)
	if err != nil {
		return DatasetVersion{}, fmt.Errorf("insert version reservation: %w", err)
	}
	if err := json.Unmarshal(metadata, &version.SourceInfo); err != nil {
		return DatasetVersion{}, fmt.Errorf("decode version metadata: %w", err)
	}
	if datasetState != "UPLOADING" {
		if _, err := tx.Exec(ctx, `UPDATE datasets SET state = 'UPLOADING', updated_at = NOW() WHERE id = $1`, reservation.DatasetID); err != nil {
			return DatasetVersion{}, fmt.Errorf("mark dataset uploading: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO dataset_state_history (dataset_id, from_state, to_state, reason, actor_user_id) VALUES ($1, $2, 'UPLOADING', 'version_upload_started', $3)`, reservation.DatasetID, datasetState, reservation.CreatedBy); err != nil {
			return DatasetVersion{}, fmt.Errorf("record upload start history: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return DatasetVersion{}, fmt.Errorf("commit version reservation: %w", err)
	}
	return version, nil
}

func (r *Repository) FindVersion(ctx context.Context, datasetID, versionID uuid.UUID) (DatasetVersion, error) {
	version, err := scanVersion(r.pool.QueryRow(ctx, versionSelect+`WHERE v.dataset_id = $1 AND v.id = $2`, datasetID, versionID), false)
	if errors.Is(err, pgx.ErrNoRows) {
		return DatasetVersion{}, ErrVersionNotFound
	}
	if err != nil {
		return DatasetVersion{}, err
	}
	return version, nil
}

func (r *Repository) FindVersionByID(ctx context.Context, versionID uuid.UUID) (DatasetVersion, error) {
	version, err := scanVersion(r.pool.QueryRow(ctx, versionSelect+`WHERE v.id = $1`, versionID), false)
	if errors.Is(err, pgx.ErrNoRows) {
		return DatasetVersion{}, ErrVersionNotFound
	}
	if err != nil {
		return DatasetVersion{}, err
	}
	return version, nil
}

func (r *Repository) FinalizeVersion(ctx context.Context, datasetID, versionID, actorID uuid.UUID, size int64, checksum string) (DatasetVersion, error) {
	return r.finalizeVersion(ctx, datasetID, versionID, actorID, size, checksum, uuid.Nil, uuid.Nil)
}

func (r *Repository) FinalizeVersionForUpload(ctx context.Context, datasetID, versionID, actorID uuid.UUID, size int64, checksum string, sessionID, operationToken uuid.UUID) (DatasetVersion, error) {
	return r.finalizeVersion(ctx, datasetID, versionID, actorID, size, checksum, sessionID, operationToken)
}

func (r *Repository) finalizeVersion(ctx context.Context, datasetID, versionID, actorID uuid.UUID, size int64, checksum string, sessionID, operationToken uuid.UUID) (DatasetVersion, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return DatasetVersion{}, fmt.Errorf("begin version finalization: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var datasetState string
	var deletedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT state, deleted_at FROM datasets WHERE id = $1 FOR UPDATE`, datasetID).Scan(&datasetState, &deletedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DatasetVersion{}, ErrDatasetNotFound
		}
		return DatasetVersion{}, fmt.Errorf("lock dataset for finalization: %w", err)
	}
	if deletedAt != nil || datasetState == "DELETED" {
		return DatasetVersion{}, ErrDatasetDeleted
	}
	if sessionID != uuid.Nil {
		var sessionState string
		var currentToken *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT state, operation_token FROM upload_sessions WHERE id = $1 AND dataset_id = $2 AND version_id = $3 FOR UPDATE`, sessionID, datasetID, versionID).Scan(&sessionState, &currentToken); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return DatasetVersion{}, ErrVersionState
			}
			return DatasetVersion{}, fmt.Errorf("lock multipart session for finalization: %w", err)
		}
		if sessionState != "COMPLETING" || currentToken == nil || *currentToken != operationToken {
			return DatasetVersion{}, ErrVersionState
		}
	}
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM dataset_versions WHERE id = $1 AND dataset_id = $2 FOR UPDATE`, versionID, datasetID).Scan(&state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DatasetVersion{}, ErrVersionNotFound
		}
		return DatasetVersion{}, fmt.Errorf("lock version for finalization: %w", err)
	}
	if state == "AVAILABLE" {
		var existingSize int64
		var existingChecksum string
		if err := tx.QueryRow(ctx, `SELECT size_bytes, checksum_sha256 FROM dataset_versions WHERE id = $1`, versionID).Scan(&existingSize, &existingChecksum); err != nil {
			return DatasetVersion{}, fmt.Errorf("read finalized version metadata: %w", err)
		}
		if existingSize != size || existingChecksum != checksum {
			return DatasetVersion{}, ErrVersionState
		}
		version, err := scanVersion(tx.QueryRow(ctx, versionSelect+`WHERE v.id = $1`, versionID), false)
		if err != nil {
			return DatasetVersion{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return DatasetVersion{}, fmt.Errorf("commit idempotent version finalization: %w", err)
		}
		return version, nil
	}
	if state != "UPLOADING" {
		return DatasetVersion{}, ErrVersionState
	}
	if _, err := tx.Exec(ctx, `UPDATE dataset_versions SET state = 'AVAILABLE', size_bytes = $1, checksum_sha256 = $2, available_at = NOW() WHERE id = $3`, size, checksum, versionID); err != nil {
		return DatasetVersion{}, fmt.Errorf("finalize dataset version: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE datasets SET state = 'AVAILABLE', updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, datasetID); err != nil {
		return DatasetVersion{}, fmt.Errorf("mark dataset available: %w", err)
	}
	var actor any
	if actorID != uuid.Nil {
		actor = actorID
	}
	if _, err := tx.Exec(ctx, `INSERT INTO dataset_state_history (dataset_id, from_state, to_state, reason, actor_user_id) VALUES ($1, 'UPLOADING', 'AVAILABLE', 'version_upload_completed', $2)`, datasetID, actor); err != nil {
		return DatasetVersion{}, fmt.Errorf("record upload completion history: %w", err)
	}
	version, err := scanVersion(tx.QueryRow(ctx, versionSelect+`WHERE v.id = $1`, versionID), false)
	if err != nil {
		return DatasetVersion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DatasetVersion{}, fmt.Errorf("commit version finalization: %w", err)
	}
	return version, nil
}

func (r *Repository) AbortVersion(ctx context.Context, datasetID, versionID, actorID uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin version abort: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var datasetState string
	if err := tx.QueryRow(ctx, `SELECT state FROM datasets WHERE id = $1 FOR UPDATE`, datasetID).Scan(&datasetState); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDatasetNotFound
		}
		return fmt.Errorf("lock dataset for abort: %w", err)
	}
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM dataset_versions WHERE id = $1 AND dataset_id = $2 FOR UPDATE`, versionID, datasetID).Scan(&state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVersionNotFound
		}
		return fmt.Errorf("lock version for abort: %w", err)
	}
	if state != "UPLOADING" {
		return ErrVersionState
	}
	if _, err := tx.Exec(ctx, `DELETE FROM dataset_versions WHERE id = $1`, versionID); err != nil {
		return fmt.Errorf("delete staged version: %w", err)
	}
	nextState := datasetState
	if datasetState != "DELETED" {
		if err := tx.QueryRow(ctx, `SELECT CASE WHEN EXISTS (SELECT 1 FROM dataset_versions WHERE dataset_id = $1 AND state = 'AVAILABLE') THEN 'AVAILABLE' ELSE 'REGISTERED' END`, datasetID).Scan(&nextState); err != nil {
			return fmt.Errorf("restore dataset state: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE datasets SET state = $1, updated_at = NOW() WHERE id = $2 AND state <> 'DELETED'`, nextState, datasetID); err != nil {
		return fmt.Errorf("update aborted dataset state: %w", err)
	}
	var actor any
	if actorID != uuid.Nil {
		actor = actorID
	}
	if _, err := tx.Exec(ctx, `INSERT INTO dataset_state_history (dataset_id, from_state, to_state, reason, actor_user_id) VALUES ($1, 'UPLOADING', $2, 'version_upload_aborted', $3)`, datasetID, nextState, actor); err != nil {
		return fmt.Errorf("record version abort history: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit version abort: %w", err)
	}
	return nil
}

func (r *Repository) FailVersion(ctx context.Context, datasetID, versionID, actorID uuid.UUID, reason string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin version failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var datasetState string
	if err := tx.QueryRow(ctx, `SELECT state FROM datasets WHERE id = $1 FOR UPDATE`, datasetID).Scan(&datasetState); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDatasetNotFound
		}
		return fmt.Errorf("lock dataset for failure: %w", err)
	}
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM dataset_versions WHERE id = $1 AND dataset_id = $2 FOR UPDATE`, versionID, datasetID).Scan(&state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVersionNotFound
		}
		return fmt.Errorf("lock version for failure: %w", err)
	}
	if state == "FAILED" {
		return nil
	}
	if state != "UPLOADING" {
		return ErrVersionState
	}
	if _, err := tx.Exec(ctx, `UPDATE dataset_versions SET state = 'FAILED' WHERE id = $1`, versionID); err != nil {
		return fmt.Errorf("mark dataset version failed: %w", err)
	}
	nextDatasetState := datasetState
	if datasetState != "DELETED" {
		if err := tx.QueryRow(ctx, `SELECT CASE WHEN EXISTS (SELECT 1 FROM dataset_versions WHERE dataset_id = $1 AND state = 'AVAILABLE') THEN 'AVAILABLE' ELSE 'REGISTERED' END`, datasetID).Scan(&nextDatasetState); err != nil {
			return fmt.Errorf("restore dataset state after version failure: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE datasets SET state = $1, updated_at = NOW() WHERE id = $2 AND state <> 'DELETED'`, nextDatasetState, datasetID); err != nil {
		return fmt.Errorf("update dataset after version failure: %w", err)
	}
	var actor any
	if actorID != uuid.Nil {
		actor = actorID
	}
	if _, err := tx.Exec(ctx, `INSERT INTO dataset_state_history (dataset_id, from_state, to_state, reason, actor_user_id) VALUES ($1, 'UPLOADING', $2, $3, $4)`, datasetID, nextDatasetState, truncateStateReason(reason), actor); err != nil {
		return fmt.Errorf("record version failure history: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit version failure: %w", err)
	}
	return nil
}

func truncateStateReason(reason string) string {
	if len(reason) > 200 {
		return reason[:200]
	}
	if reason == "" {
		return "version_upload_failed"
	}
	return reason
}

const versionSelect = `
SELECT v.id, v.dataset_id, v.version_number, v.state, v.original_filename, v.content_type, v.format, v.size_bytes, v.object_key, v.checksum_sha256,
       v.row_count, v.column_count, v.encoding, v.compression_type, v.source_info, v.artifact_prefix, v.created_by, v.created_at, v.available_at
FROM dataset_versions v
`

func (r *Repository) ListVersions(ctx context.Context, datasetID uuid.UUID) ([]DatasetVersion, error) {
	rows, err := r.pool.Query(ctx, versionSelect+`WHERE v.dataset_id = $1 ORDER BY v.version_number DESC`, datasetID)
	if err != nil {
		return nil, fmt.Errorf("list dataset versions: %w", err)
	}
	defer rows.Close()
	versions := make([]DatasetVersion, 0)
	for rows.Next() {
		version, err := scanVersion(rows, true)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dataset versions: %w", err)
	}
	return versions, nil
}

type versionRow interface {
	Scan(...any) error
}

func scanVersion(row versionRow, _ bool) (DatasetVersion, error) {
	var version DatasetVersion
	var format string
	var metadata []byte
	if err := row.Scan(&version.ID, &version.DatasetID, &version.VersionNumber, &version.State, &version.OriginalFilename, &version.ContentType, &format, &version.SizeBytes, &version.ObjectKey, &version.ChecksumSHA256, &version.RowCount, &version.ColumnCount, &version.Encoding, &version.CompressionType, &metadata, &version.ArtifactPrefix, &version.CreatedBy, &version.CreatedAt, &version.AvailableAt); err != nil {
		return DatasetVersion{}, fmt.Errorf("scan dataset version: %w", err)
	}
	version.Format = upload.Format(format)
	if len(metadata) == 0 {
		version.SourceInfo = map[string]any{}
	} else if err := json.Unmarshal(metadata, &version.SourceInfo); err != nil {
		return DatasetVersion{}, fmt.Errorf("decode dataset version source info: %w", err)
	}
	return version, nil
}
