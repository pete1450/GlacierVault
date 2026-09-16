package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

const rusticBin = "rustic"

// RingBuffer holds the last N log lines for a job, safe for concurrent use.
type RingBuffer struct {
	mu    sync.Mutex
	lines []string
	cap   int
	pos   int
}

func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{lines: make([]string, capacity), cap: capacity}
}

func (r *RingBuffer) Write(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines[r.pos%r.cap] = line
	r.pos++
}

func (r *RingBuffer) Lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, r.cap)
	start := 0
	if r.pos > r.cap {
		start = r.pos % r.cap
	}
	for i := 0; i < min(r.pos, r.cap); i++ {
		out = append(out, r.lines[(start+i)%r.cap])
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// JobBuffers stores per-job log ring buffers.
var (
	jobBufMu sync.Mutex
	jobBufs  = map[int64]*RingBuffer{}
)

func GetBuffer(jobID int64) *RingBuffer {
	jobBufMu.Lock()
	defer jobBufMu.Unlock()
	if b, ok := jobBufs[jobID]; ok {
		return b
	}
	b := NewRingBuffer(1000)
	jobBufs[jobID] = b
	return b
}

// Snapshot is a parsed rustic snapshot JSON entry.
type Snapshot struct {
	ID       string   `json:"id"`
	Time     string   `json:"time"`
	Hostname string   `json:"hostname"`
	Tags     []string `json:"tags"`
	Paths    []string `json:"paths"`
	Summary  *struct {
		TotalBytesProcessed int64 `json:"total_bytes_processed"`
		TotalFilesProcessed int64 `json:"total_files_processed"`
	} `json:"summary"`
}

// FileEntry is a parsed rustic ls JSON entry.
type FileEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Mtime string `json:"mtime"`
	Type  string `json:"type"` // "file" or "dir"
}

// Engine wraps rustic commands with the repository config path.
type Engine struct {
	configPath string
}

func New(configPath string) *Engine {
	return &Engine{configPath: configPath}
}

func (e *Engine) baseArgs() []string {
	// rustic -P <path> appends .toml to find the config file, so strip the extension.
	return []string{"-P", strings.TrimSuffix(e.configPath, ".toml")}
}

// run runs rustic with args, returns stdout bytes.
func (e *Engine) run(ctx context.Context, buf *RingBuffer, args ...string) ([]byte, error) {
	cmdArgs := append(e.baseArgs(), args...)
	cmd := exec.CommandContext(ctx, rusticBin, cmdArgs...)

	var stdout bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		if buf != nil {
			buf.Write(fmt.Sprintf("[stderr] %s", stderrBuf.String()))
		}
		return nil, fmt.Errorf("rustic %s: %w — %s", strings.Join(args, " "), err, stderrBuf.String())
	}
	return stdout.Bytes(), nil
}

// runStreaming runs rustic and writes each output line to buf, returning combined output.
func (e *Engine) runStreaming(ctx context.Context, buf *RingBuffer, args ...string) ([]byte, error) {
	cmdArgs := append(e.baseArgs(), args...)
	cmd := exec.CommandContext(ctx, rusticBin, cmdArgs...)

	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	var output bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			line := scanner.Text()
			output.WriteString(line + "\n")
			if buf != nil {
				buf.Write(line)
			}
		}
	}()

	runErr := cmd.Run()
	pw.Close()
	<-done
	pr.Close()

	if runErr != nil {
		return output.Bytes(), fmt.Errorf("rustic %s: %w", strings.Join(args, " "), runErr)
	}
	return output.Bytes(), nil
}

// InitRepository runs rustic init for a new cold-storage repository.
func (e *Engine) InitRepository(ctx context.Context, buf *RingBuffer) error {
	_, err := e.runStreaming(ctx, buf, "init")
	return err
}

// RunBackup executes rustic backup and streams output to buf.
func (e *Engine) RunBackup(ctx context.Context, buf *RingBuffer, sourcePaths []string, tags []string) error {
	args := []string{"backup"}
	for _, t := range tags {
		args = append(args, "--tag", t)
	}
	args = append(args, sourcePaths...)
	_, err := e.runStreaming(ctx, buf, args...)
	return err
}

// ListSnapshots returns all snapshots from the repository.
//
// rustic's `snapshots --json` emits snapshots grouped by (hostname, label,
// paths) as [[group, [snapshots...]], ...]; we flatten all groups into one
// list. A plain flat array is accepted as a fallback.
func (e *Engine) ListSnapshots(ctx context.Context) ([]Snapshot, error) {
	out, err := e.run(ctx, nil, "snapshots", "--json")
	if err != nil {
		return nil, err
	}

	// Try the grouped format first.
	var groups []json.RawMessage
	if err := json.Unmarshal(out, &groups); err != nil {
		return nil, fmt.Errorf("parse snapshots: %w", err)
	}
	var snaps []Snapshot
	for _, g := range groups {
		var pair []json.RawMessage
		if err := json.Unmarshal(g, &pair); err != nil || len(pair) != 2 {
			// Fall back: maybe it's a flat snapshot list after all.
			var flat []Snapshot
			if ferr := json.Unmarshal(out, &flat); ferr != nil {
				return nil, fmt.Errorf("parse snapshots: %w", err)
			}
			return flat, nil
		}
		var groupSnaps []Snapshot
		if err := json.Unmarshal(pair[1], &groupSnaps); err != nil {
			return nil, fmt.Errorf("parse snapshots: %w", err)
		}
		snaps = append(snaps, groupSnaps...)
	}
	return snaps, nil
}

// ListFiles returns file entries for a snapshot.
//
// rustic 0.9.x `ls --json` emits a single JSON array of relative path strings.
// Older/newer versions may emit one JSON object per line (NDJSON) with
// name/path/size/mtime/type fields; both formats are accepted. For the path
// array format, size/mtime are unknown and directories are detected by the
// "is a strict prefix of another path" heuristic.
func (e *Engine) ListFiles(ctx context.Context, snapshotID string) ([]FileEntry, error) {
	out, err := e.run(ctx, nil, "ls", snapshotID, "--json")
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}

	// Format 1: single JSON array of path strings.
	if strings.HasPrefix(trimmed, "[") {
		var paths []string
		if err := json.Unmarshal([]byte(trimmed), &paths); err != nil {
			return nil, fmt.Errorf("parse ls paths: %w", err)
		}
		return pathsToEntries(paths), nil
	}

	// Format 2: one JSON object per line.
	var entries []FileEntry
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry FileEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// pathsToEntries converts relative path strings into FileEntries. A path is
// considered a directory when it is a strict prefix (path + "/") of another
// path in the list.
func pathsToEntries(paths []string) []FileEntry {
	entries := make([]FileEntry, 0, len(paths))
	for _, p := range paths {
		isDir := false
		prefix := strings.TrimSuffix(p, "/") + "/"
		for _, other := range paths {
			if other != p && strings.HasPrefix(other, prefix) {
				isDir = true
				break
			}
		}
		entries = append(entries, FileEntry{
			Path: p,
			Name: p[strings.LastIndex(p, "/")+1:],
			Type: map[bool]string{true: "dir", false: "file"}[isDir],
		})
	}
	return entries
}

// RunRestore executes rustic restore for a snapshot to destination.
func (e *Engine) RunRestore(ctx context.Context, buf *RingBuffer, snapshotID, destination string, paths []string) error {
	args := []string{"restore", snapshotID, "--target", destination}
	for _, p := range paths {
		args = append(args, "--glob", p)
	}
	_, err := e.runStreaming(ctx, buf, args...)
	return err
}
