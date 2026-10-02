package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/glaciervault/api/internal/engine"
)

// Catalog syncs rustic snapshot metadata into SQLite.
type Catalog struct {
	db     *sql.DB
	engine *engine.Engine
}

func New(db *sql.DB, eng *engine.Engine) *Catalog {
	return &Catalog{db: db, engine: eng}
}

// SyncAfterBackup refreshes the snapshot catalog from the repository.
// New snapshots are inserted; existing ones are skipped.
func (c *Catalog) SyncAfterBackup(ctx context.Context, backupDefID int64) error {
	snaps, err := c.engine.ListSnapshots(ctx)
	if err != nil {
		return fmt.Errorf("list snapshots: %w", err)
	}

	for _, s := range snaps {
		if err := c.upsertSnapshot(ctx, s, backupDefID); err != nil {
			return err
		}
	}
	return nil
}

func (c *Catalog) upsertSnapshot(ctx context.Context, s engine.Snapshot, backupDefID int64) error {
	var totalSize int64
	var fileCount int64
	if s.Summary != nil {
		totalSize = s.Summary.TotalBytesProcessed
		fileCount = s.Summary.TotalFilesProcessed
	}

	tagsJSON := "[]"
	if len(s.Tags) > 0 {
		if b, err := json.Marshal(s.Tags); err == nil {
			tagsJSON = string(b)
		}
	}

	_, err := c.db.ExecContext(ctx, `
		INSERT INTO snapshots (snapshot_id, backup_def_id, hostname, tags, total_size, file_count, backup_time, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(snapshot_id) DO UPDATE SET
			total_size = excluded.total_size,
			file_count = excluded.file_count,
			synced_at  = excluded.synced_at
	`, s.ID, backupDefID, s.Hostname, tagsJSON, totalSize, fileCount, s.Time, time.Now().UTC())
	return err
}

// IndexSnapshot lazily populates the file_index for a snapshot on first browse.
// It delegates to indexSnapshotWithProgress with no progress tracking; the
// HTTP path uses EnsureIndexing for background progress instead.
func (c *Catalog) IndexSnapshot(ctx context.Context, snapshotRowID int64, rusticSnapshotID string) error {
	return c.indexSnapshotWithProgress(ctx, snapshotRowID, rusticSnapshotID, nil)
}

// RebuildCatalog drops and re-syncs all snapshot metadata from the repository.
func (c *Catalog) RebuildCatalog(ctx context.Context) error {
	snaps, err := c.engine.ListSnapshots(ctx)
	if err != nil {
		return fmt.Errorf("list snapshots for rebuild: %w", err)
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Clear existing index; snapshots are upserted below.
	if _, err := tx.ExecContext(ctx, `DELETE FROM file_index`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	for _, s := range snaps {
		if err := c.upsertSnapshot(ctx, s, 0); err != nil {
			return err
		}
	}

	// Re-index all snapshots.
	rows, err := c.db.QueryContext(ctx, `SELECT id, snapshot_id FROM snapshots`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		id         int64
		snapshotID string
	}
	var toIndex []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.snapshotID); err != nil {
			return err
		}
		toIndex = append(toIndex, r)
	}
	rows.Close()

	for _, r := range toIndex {
		if err := c.IndexSnapshot(ctx, r.id, r.snapshotID); err != nil {
			return fmt.Errorf("index snapshot %s: %w", r.snapshotID, err)
		}
	}
	return nil
}
