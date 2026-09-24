package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// argsCapturingRustic is a stub `rustic` that records every argv it sees
// (after the -P profile args) into $CAPTURE_FILE, one line per invocation.
const argsCapturingRustic = `#!/bin/sh
echo "$@" >> "$CAPTURE_FILE"
exit 0
`

func writeArgsCapturingRustic(t *testing.T, captureFile string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "rustic")
	if err := os.WriteFile(p, []byte(argsCapturingRustic), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CAPTURE_FILE", captureFile)
}

func lastCapturedArgs(t *testing.T, captureFile string) string {
	t.Helper()
	raw, err := os.ReadFile(captureFile)
	if err != nil {
		t.Fatalf("read capture file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	return lines[len(lines)-1]
}

func TestRunBackup_PassesCompressionLevel(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "args.txt")
	writeArgsCapturingRustic(t, capture)
	e := New("/nonexistent/rustic.toml")
	buf := NewRingBuffer(16)

	if err := e.RunBackup(context.Background(), buf, []string{"/data"}, []string{"docs"}, 7); err != nil {
		t.Fatalf("RunBackup: %v", err)
	}
	got := lastCapturedArgs(t, capture)
	if !strings.Contains(got, "--set-compression 7") {
		t.Errorf("expected --set-compression 7 in args, got: %q", got)
	}
	if !strings.Contains(got, "--tag docs") {
		t.Errorf("expected --tag docs in args, got: %q", got)
	}
	if !strings.Contains(got, "backup") || !strings.Contains(got, "/data") {
		t.Errorf("expected backup command with source path, got: %q", got)
	}
}

func TestRunBackup_SkipsInvalidCompressionLevel(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "args.txt")
	writeArgsCapturingRustic(t, capture)
	e := New("/nonexistent/rustic.toml")

	for _, level := range []int{0, -3, 23, 100} {
		os.Remove(capture)
		buf := NewRingBuffer(16)
		if err := e.RunBackup(context.Background(), buf, []string{"/data"}, []string{"docs"}, level); err != nil {
			t.Fatalf("RunBackup(%d): %v", level, err)
		}
		got := lastCapturedArgs(t, capture)
		if strings.Contains(got, "--set-compression") {
			t.Errorf("level %d: must not pass --set-compression, got: %q", level, got)
		}
	}
}
