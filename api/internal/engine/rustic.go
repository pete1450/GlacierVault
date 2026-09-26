package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
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
	return e.runStreamingWith(ctx, buf, nil, "", args...)
}

// runStreamingWith is runStreaming with extra environment and an optional
// working directory for the child process.
func (e *Engine) runStreamingWith(ctx context.Context, buf *RingBuffer, env []string, dir string, args ...string) ([]byte, error) {
	return e.runStreamingWithProfile(ctx, buf, env, dir, e.configPath, args...)
}

// runStreamingWithProfile is runStreamingWith using an explicit rustic
// profile (config file). It exists so restores can use a per-job config that
// points the cold backend at the localhost CloudFront proxy while every
// other operation keeps using the standard config.
func (e *Engine) runStreamingWithProfile(ctx context.Context, buf *RingBuffer, env []string, dir, configPath string, args ...string) ([]byte, error) {
	return e.runStreamingWithProfileHook(ctx, buf, env, dir, configPath, nil, args...)
}

// runStreamingWithProfileHook is runStreamingWithProfile with a per-line
// callback invoked for every line the child process emits. The restore
// manager uses it to capture the S3 Batch job ID from warmup-s3-archives'
// output as soon as the tool submits the job.
func (e *Engine) runStreamingWithProfileHook(ctx context.Context, buf *RingBuffer, env []string, dir, configPath string, hook func(string), args ...string) ([]byte, error) {
	cmdArgs := append([]string{"-P", strings.TrimSuffix(configPath, ".toml")}, args...)
	cmd := exec.CommandContext(ctx, rusticBin, cmdArgs...)
	if len(env) > 0 {
		cmd.Env = env
	}
	if dir != "" {
		cmd.Dir = dir
	}

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
			if hook != nil {
				hook(line)
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

// Pack size targets for newly initialized repositories. Larger packs mean
// fewer objects in the cold bucket: fewer PUTs at backup time, less
// per-object metadata overhead (Deep Archive bills ~40 KiB per object), and
// fewer restore requests when retrieving. Tradeoffs: higher memory use during
// backup (rustic buffers whole packs, several in parallel) and coarser
// restore granularity for partial restores. The grow factor still increases
// these targets automatically as the repository grows.
const (
	defaultDataPackSize = "512MiB"
	defaultTreePackSize = "32MiB"
)

// InitRepository runs rustic init for a new cold-storage repository.
func (e *Engine) InitRepository(ctx context.Context, buf *RingBuffer) error {
	_, err := e.runStreaming(ctx, buf, "init",
		"--set-datapack-size", defaultDataPackSize,
		"--set-treepack-size", defaultTreePackSize)
	return err
}

// RunBackup executes rustic backup and streams output to buf.
// compressionLevel is a zstd level (1-22); values outside that range are
// ignored and rustic's default applies.
func (e *Engine) RunBackup(ctx context.Context, buf *RingBuffer, sourcePaths []string, tags []string, compressionLevel int) error {
	args := []string{"backup"}
	for _, t := range tags {
		args = append(args, "--tag", t)
	}
	if compressionLevel >= 1 && compressionLevel <= 22 {
		args = append(args, "--set-compression", strconv.Itoa(compressionLevel))
	}
	args = append(args, sourcePaths...)
	_, err := e.runStreaming(ctx, buf, args...)
	return err
}

// ListSnapshots returns all snapshots from the repository.
//
// `snapshots --json` grouping format has changed across rustic versions:
//   - rustic 0.11+: [{"group_key": {...}, "snapshots": [...]}, ...]
//   - rustic 0.9.x:  [[group, [snapshots...]], ...]
//
// A plain flat array is accepted as a final fallback. All shapes are
// flattened into one list.
func (e *Engine) ListSnapshots(ctx context.Context) ([]Snapshot, error) {
	out, err := e.run(ctx, nil, "snapshots", "--json")
	if err != nil {
		return nil, err
	}
	return parseSnapshots(out)
}

// parseSnapshots flattens `snapshots --json` output into one snapshot list.
// See ListSnapshots for the version-dependent shapes handled.
func parseSnapshots(out []byte) ([]Snapshot, error) {
	var groups11 []struct {
		Snapshots []Snapshot `json:"snapshots"`
	}
	if err := json.Unmarshal(out, &groups11); err == nil && groups11 != nil {
		var snaps []Snapshot
		ok := true
		for _, g := range groups11 {
			if g.Snapshots == nil {
				ok = false
				break
			}
			snaps = append(snaps, g.Snapshots...)
		}
		if ok {
			return snaps, nil
		}
	}

	// Shape 2: rustic 0.9.x grouped [group, [snapshots]] pairs.
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

// RestoreOptions carries the Glacier warm-up configuration for RunRestore.
type RestoreOptions struct {
	// Warmup enables the warm-up phase via the warmup-s3-archives tool,
	// invoked as rustic's --warm-up-command.
	Warmup bool
	// Env is extra environment for the rustic process. The warmup tool
	// resolves AWS credentials on its own (rustic does not pass its
	// backend credentials through), so AWS_* must be present here.
	Env []string
	// Dir is the working directory for the rustic process.
	// warmup-s3-archives reads warmup-s3-archives-config.toml from it.
	Dir string
	// ConfigPath optionally overrides the rustic profile (config file) for
	// this restore only. Used to point the cold backend at the localhost
	// CloudFront proxy; empty means the engine's default config.
	ConfigPath string
	// LineHook, when set, is invoked for every line the rustic process
	// emits. The restore manager uses it to capture the S3 Batch job ID
	// from warmup-s3-archives' output as soon as the tool submits the job,
	// so a container restart can re-attach to the in-flight warmup.
	LineHook func(string)
}

// RunRestore executes rustic restore for a snapshot to destination.
//
// Destination is positional (rustic has no --target flag). When
// opts.Warmup is set, rustic warms the needed data packs first by invoking
// `glaciervault-warmup` (a wrapper around warmup-s3-archives that retries the
// tool on its SQS wait timeout) with the S3 keys of the needed packs in
// batches; the tool submits S3 Batch restore jobs and blocks until Glacier
// has the packs available, then rustic proceeds with the download.
func (e *Engine) RunRestore(ctx context.Context, buf *RingBuffer, snapshotID, destination string, paths []string, opts RestoreOptions) error {
	args := []string{"restore", snapshotID, destination}
	for _, p := range paths {
		args = append(args, "--glob", p)
	}
	if opts.Warmup {
		args = append(args,
			"--warm-up-command", "glaciervault-warmup %paths",
			"--warm-up-batch", "1000",
		)
	}
	_, err := e.runStreamingWithProfileHook(ctx, buf, opts.Env, opts.Dir, e.profileForRestore(opts), opts.LineHook, args...)
	return err
}

// profileForRestore selects the rustic config for a restore: the per-restore
// override when set, otherwise the engine default.
func (e *Engine) profileForRestore(opts RestoreOptions) string {
	if opts.ConfigPath != "" {
		return opts.ConfigPath
	}
	return e.configPath
}

// RepoInfo summarizes repository storage usage.
type RepoInfo struct {
	// TotalBytes is the size of all files in the repository (packs + index +
	// snapshots + keys). This is the billed storage footprint.
	TotalBytes int64 `json:"totalBytes"`
	// PackBytes is the size of data/tree pack files, the bulk of the storage.
	PackBytes int64 `json:"packBytes"`
	// IndexBytes is the size of index files.
	IndexBytes int64 `json:"indexBytes"`
	// SnapshotCount is the number of snapshots in the repository.
	SnapshotCount int64 `json:"snapshotCount"`
}

// ErrSnapshotAlreadyGone is returned by ForgetSnapshot when the snapshot
// file is no longer present in the repository (neither hot nor cold
// backend). The desired end state — the snapshot being gone — is already
// true, so callers should treat this as success and clean up their own
// bookkeeping.
var ErrSnapshotAlreadyGone = errors.New("snapshot already removed from repository")

// ForgetSnapshot removes a single snapshot from the repository by rustic ID.
// When prune is true, `forget --prune` runs the prune step automatically,
// reclaiming space from data that is no longer referenced by any snapshot.
//
// rustic's hot/cold backend is not atomic across backends: forget removes the
// snapshot file from both and fails if either copy is already missing — even
// when it just removed the other one. If the failure is a not-found on the
// snapshot file and a fresh listing no longer shows the snapshot, the delete
// is effectively complete, so this returns ErrSnapshotAlreadyGone instead of
// an error.
func (e *Engine) ForgetSnapshot(ctx context.Context, snapshotID string, prune bool) error {
	args := []string{"forget", snapshotID}
	if prune {
		args = append(args, "--prune")
	}
	_, err := e.run(ctx, nil, args...)
	if err == nil {
		return nil
	}
	if !isSnapshotNotFoundErr(err) {
		return err
	}
	// The snapshot file is gone from at least one backend. Confirm it is
	// really unlistable (not just a hot/cold skew) before calling it done.
	listed, lerr := e.ListSnapshots(ctx)
	if lerr != nil {
		return err // cannot verify; surface the original failure
	}
	for _, s := range listed {
		if s.ID == snapshotID || strings.HasPrefix(s.ID, snapshotID) || strings.HasPrefix(snapshotID, s.ID) {
			return err // still present — a real failure
		}
	}
	return ErrSnapshotAlreadyGone
}

// isSnapshotNotFoundErr reports whether err looks like rustic failing because
// the snapshot file does not exist (S3 NoSuchKey / local ENOENT / opendal
// NotFound on the snapshots/ path).
func isSnapshotNotFoundErr(err error) bool {
	msg := err.Error()
	if !strings.Contains(msg, "snapshots/") {
		return false
	}
	for _, sig := range []string{"NoSuchKey", "No such file or directory", "NotFound"} {
		if strings.Contains(msg, sig) {
			return true
		}
	}
	return false
}

// PruneRepo removes unreferenced data and repacks pack files.
func (e *Engine) PruneRepo(ctx context.Context) error {
	_, err := e.run(ctx, nil, "prune")
	return err
}

// GetRepoInfo returns repository storage statistics.
//
// It runs `rustic repoinfo --json` (available since rustic 0.6.0). The exact
// JSON schema is not stable across versions, so parsing is deliberately
// tolerant: several shapes are probed, and if JSON parsing yields nothing,
// the human-readable table output is parsed as a fallback.
func (e *Engine) GetRepoInfo(ctx context.Context) (RepoInfo, error) {
	out, err := e.run(ctx, nil, "repoinfo", "--json")
	if err != nil {
		// Older rustic builds lack --json; fall back to text tables.
		if tout, terr := e.run(ctx, nil, "repoinfo"); terr == nil {
			return parseRepoInfoText(tout)
		}
		return RepoInfo{}, err
	}
	if info, ok := parseRepoInfoJSON(out); ok {
		return info, nil
	}
	// JSON didn't yield anything useful; try the text tables.
	tout, terr := e.run(ctx, nil, "repoinfo")
	if terr != nil {
		return RepoInfo{}, fmt.Errorf("parse repoinfo: unrecognized output")
	}
	return parseRepoInfoText(tout)
}

// parseRepoInfoJSON extracts RepoInfo from `repoinfo --json` output,
// tolerating several schema shapes.
func parseRepoInfoJSON(out []byte) (RepoInfo, bool) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out, &doc); err != nil {
		return RepoInfo{}, false
	}
	var info RepoInfo
	// "files" may be an array of {type,count,size} or an object keyed by type.
	if raw, ok := doc["files"]; ok {
		stats := parseFileStats(raw)
		info.TotalBytes = stats["total"]
		info.PackBytes = stats["pack"]
		info.IndexBytes = stats["index"]
		info.SnapshotCount = stats["snapshot_count"]
		// Some schemas nest per-type objects without a "total" entry; sum parts.
		if info.TotalBytes == 0 {
			info.TotalBytes = info.PackBytes + info.IndexBytes + stats["key"] + stats["snapshot"]
		}
	}
	if info.TotalBytes == 0 && info.PackBytes == 0 && info.SnapshotCount == 0 {
		return RepoInfo{}, false
	}
	return info, true
}

// parseFileStats normalizes the "files" section of repoinfo --json into a
// map from lower-cased file type to its size in bytes, and from type to
// count. The returned map keys are "type:size" and "type:count".
func parseFileStats(raw json.RawMessage) map[string]int64 {
	m := map[string]int64{}
	set := func(typ string, size, count int64) {
		t := strings.ToLower(typ)
		m[t+":size"] = size
		m[t+":count"] = count
	}
	// Shape 1: array of {"type": ..., "count": ..., "size"|"total_size": ...}.
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		for _, e := range arr {
			typ := unquote(e["type"])
			if typ == "" {
				typ = unquote(e["tpe"]) // rustic 0.9.x uses "tpe"
			}
			size := firstInt64(e, "size", "total_size", "bytes", "total_bytes")
			count := firstInt64(e, "count")
			if typ != "" {
				set(typ, size, count)
			}
		}
		return flattenStats(m)
	}
	// Shape 1b: {"repo": [...]} — the actual rustic 0.9.4 repoinfo schema.
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapper); err == nil {
		if repoRaw, ok := wrapper["repo"]; ok {
			return parseFileStats(repoRaw)
		}
	}
	// Shape 2: object keyed by type, values with size/count fields.
	var obj map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		for typ, fields := range obj {
			size := firstInt64(fields, "size", "total_size", "bytes", "total_bytes")
			count := firstInt64(fields, "count")
			set(typ, size, count)
		}
		return flattenStats(m)
	}
	return map[string]int64{}
}

func flattenStats(m map[string]int64) map[string]int64 {
	out := map[string]int64{}
	for k, v := range m {
		parts := strings.SplitN(k, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[1] == "size" {
			out[parts[0]] = v
		} else {
			out[parts[0]+"_count"] = v
		}
	}
	return out
}

func unquote(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func firstInt64(fields map[string]json.RawMessage, keys ...string) int64 {
	for _, k := range keys {
		raw, ok := fields[k]
		if !ok {
			continue
		}
		var n int64
		if err := json.Unmarshal(raw, &n); err == nil {
			return n
		}
		var f float64
		if err := json.Unmarshal(raw, &f); err == nil {
			return int64(f)
		}
	}
	return 0
}

// parseRepoInfoText parses the human-readable `rustic repoinfo` tables:
//
//	| File type | Count | Total Size |
//	| Key       |     1 |      363 B |
//	| Pack      |     5 |   51.5 MiB |
//	| Total     |    21 |   51.5 MiB |
func parseRepoInfoText(out []byte) (RepoInfo, error) {
	var info RepoInfo
	found := false
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
			continue
		}
		cols := strings.Split(strings.Trim(line, "|"), "|")
		if len(cols) < 3 {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(cols[0]))
		if typ == "file type" || strings.HasPrefix(typ, "---") {
			continue
		}
		size, err := parseHumanSize(strings.TrimSpace(cols[2]))
		if err != nil {
			continue
		}
		count, _ := strconv.ParseInt(strings.TrimSpace(cols[1]), 10, 64)
		switch typ {
		case "total":
			info.TotalBytes = size
		case "pack":
			info.PackBytes = size
		case "index":
			info.IndexBytes = size
		case "snapshot":
			info.SnapshotCount = count
		}
		found = true
	}
	if !found {
		return RepoInfo{}, fmt.Errorf("parse repoinfo: no table rows found")
	}
	return info, nil
}

// parseHumanSize parses sizes like "363 B", "5.9 kiB", "51.5 MiB", "1.2 GiB".
func parseHumanSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0, nil
	}
	// Split numeric part from unit.
	i := 0
	for i < len(s) && (s[i] == '.' || s[i] == ',' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	numStr := strings.ReplaceAll(s[:i], ",", "")
	unit := strings.ToLower(strings.TrimSpace(s[i:]))
	f, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("parse size %q: %w", s, err)
	}
	mult := 1.0
	switch unit {
	case "b", "byte", "bytes", "":
		mult = 1
	case "kib", "kb", "k":
		mult = 1024
	case "mib", "mb", "m":
		mult = 1024 * 1024
	case "gib", "gb", "g":
		mult = 1024 * 1024 * 1024
	case "tib", "tb", "t":
		mult = 1024 * 1024 * 1024 * 1024
	case "pib", "pb", "p":
		mult = 1024 * 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("parse size %q: unknown unit", s)
	}
	return int64(f * mult), nil
}
