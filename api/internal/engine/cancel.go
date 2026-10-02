package engine

import (
	"context"
	"sync"
)

// jobCancels tracks cancel funcs for in-progress backup jobs so the API can
// kill them on request (POST /api/jobs/{id}/cancel). Cancelling kills the
// rustic child process via its context — the same as a crash: packs
// uploaded so far stay (unindexed until the next completed backup), no
// snapshot is written, and the job row is marked 'cancelled' by the handler
// before the kill so the backup goroutine's guarded update won't overwrite
// it. Mirrors the JobBuffers registry pattern.
var (
	jobCancelMu sync.Mutex
	jobCancels  = map[int64]context.CancelFunc{}
)

// RegisterCancel records the cancel func for a running backup job.
func RegisterCancel(jobID int64, cancel context.CancelFunc) {
	jobCancelMu.Lock()
	defer jobCancelMu.Unlock()
	jobCancels[jobID] = cancel
}

// UnregisterCancel removes a job's cancel func once it finished.
func UnregisterCancel(jobID int64) {
	jobCancelMu.Lock()
	defer jobCancelMu.Unlock()
	delete(jobCancels, jobID)
}

// HasCancel reports whether a running backup job is registered.
func HasCancel(jobID int64) bool {
	jobCancelMu.Lock()
	defer jobCancelMu.Unlock()
	_, ok := jobCancels[jobID]
	return ok
}

// CancelJob kills the rustic process for a running backup job.
// Reports whether a running job was found.
func CancelJob(jobID int64) bool {
	jobCancelMu.Lock()
	cancel, ok := jobCancels[jobID]
	jobCancelMu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}
