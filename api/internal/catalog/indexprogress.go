package catalog

import (
	"context"
	"sync"

	"github.com/glaciervault/api/internal/engine"
)

// IndexProgress tracks a background snapshot-indexing job so the UI can show
// progress instead of a hanging request. All methods are safe for concurrent
// use; Snapshot() gives a consistent view.
type IndexProgress struct {
	mu       sync.Mutex
	phase    string // "discovering" (rustic ls) or "indexing" (SQLite inserts)
	done     int64
	total    int64 // from snapshots.file_count; 0 = unknown
	err      error
	finished bool
}

func (p *IndexProgress) set(phase string, done int64) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.phase = phase
	p.done = done
}

// Snapshot returns a consistent view of the progress.
func (p *IndexProgress) Snapshot() (phase string, done, total int64, err error, finished bool) {
	if p == nil {
		return "", 0, 0, nil, true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.phase, p.done, p.total, p.err, p.finished
}

func (p *IndexProgress) finish(err error) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
	p.finished = true
}

var (
	idxProgMu sync.Mutex
	// idxProg tracks in-flight indexing jobs by snapshot row ID. Finished
	// entries are kept (tiny, bounded by snapshot count) so late pollers see
	// completion; failed entries are removed to allow a retry.
	idxProg = map[int64]*IndexProgress{}
)

// IsIndexed reports whether the snapshot already has file_index rows.
func (c *Catalog) IsIndexed(ctx context.Context, snapshotRowID int64) (bool, error) {
	var count int
	if err := c.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM file_index WHERE snapshot_id = ?`, snapshotRowID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// EnsureIndexing starts background indexing for a snapshot unless it is
// already indexed or indexing. It never blocks: the caller polls
// IndexProgress.Snapshot() until finished. total is the expected entry
// count (snapshots.file_count; 0 = unknown, progress is then indeterminate).
func (c *Catalog) EnsureIndexing(snapshotRowID int64, rusticSnapshotID string, total int64) *IndexProgress {
	idxProgMu.Lock()
	defer idxProgMu.Unlock()
	if p, ok := idxProg[snapshotRowID]; ok {
		return p
	}
	p := &IndexProgress{phase: "discovering", total: total}
	idxProg[snapshotRowID] = p
	go func() {
		// Fresh context: the triggering HTTP request is long gone.
		err := c.indexSnapshotWithProgress(context.Background(), snapshotRowID, rusticSnapshotID, p)
		if err != nil {
			// Drop the entry so the next request retries.
			idxProgMu.Lock()
			delete(idxProg, snapshotRowID)
			idxProgMu.Unlock()
		}
		p.finish(err)
	}()
	return p
}

// indexSnapshotWithProgress populates file_index for a snapshot, reporting
// progress. It is idempotent-ish: the file_index rows are written in one
// transaction, so a crash or error leaves zero rows and a retry starts clean.
func (c *Catalog) indexSnapshotWithProgress(ctx context.Context, snapshotRowID int64, rusticSnapshotID string, prog *IndexProgress) error {
	// Another request may have indexed while this job was queued.
	if indexed, err := c.IsIndexed(ctx, snapshotRowID); err != nil {
		return err
	} else if indexed {
		return nil
	}

	// Phase 1: stream `rustic ls`, counting entries live instead of
	// buffering the whole listing before doing anything.
	var entries []engine.FileEntry
	streamed, err := c.engine.ListFilesStream(ctx, rusticSnapshotID, func(e engine.FileEntry) {
		entries = append(entries, e)
		prog.set("discovering", int64(len(entries)))
	})
	if err != nil {
		return err
	}
	if !streamed {
		// Single-array format (small snapshots): no streaming possible.
		if entries, err = c.engine.ListFiles(ctx, rusticSnapshotID); err != nil {
			return err
		}
		prog.set("discovering", int64(len(entries)))
	}

	// Phase 2: bulk insert, reporting every 1000 rows.
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO file_index (snapshot_id, path, size, mtime, is_dir) VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, e := range entries {
		isDir := 0
		if e.Type == "dir" {
			isDir = 1
		}
		if _, err := stmt.ExecContext(ctx, snapshotRowID, e.Path, e.Size, e.Mtime, isDir); err != nil {
			return err
		}
		if i%1000 == 0 {
			prog.set("indexing", int64(i))
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	prog.set("indexing", int64(len(entries)))
	return nil
}
