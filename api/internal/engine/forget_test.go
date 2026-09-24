package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRustic writes a stub `rustic` executable into dir. The stub fails
// `forget` with a realistic S3 not-found error on the snapshot file and
// answers `snapshots --json` with FAKE_SNAPSHOTS_JSON.
const fakeRustic = `#!/bin/sh
if [ "$3" = "forget" ]; then
  echo "[WARN] path=snapshots/$4 range=0-: read failed NotFound (persistent) at read" >&2
  echo 'error: rustic_core experienced an error related to the backend.' >&2
  echo "Message: Reading file snapshots/$4 failed in the backend." >&2
  echo 'Caused by: NotFound (persistent) at read => S3Error { code: "NoSuchKey", message: "The specified key does not exist." }' >&2
  exit 1
fi
if [ "$3" = "snapshots" ]; then
  echo "$FAKE_SNAPSHOTS_JSON"
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`

func writeFakeRustic(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "rustic")
	if err := os.WriteFile(p, []byte(fakeRustic), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestIsSnapshotNotFoundErr(t *testing.T) {
	s3err := errors.New(`rustic forget abc: exit status 1 — [WARN] path=snapshots/abc123 read failed NotFound at read => S3Error { code: "NoSuchKey", message: "The specified key does not exist." }`)
	if !isSnapshotNotFoundErr(s3err) {
		t.Error("S3 NoSuchKey on snapshots/ should be not-found")
	}
	localErr := errors.New("rustic forget abc: exit status 1 — Failed to remove the file `/hot/snapshots/abc123`. Caused by: No such file or directory (os error 2)")
	if !isSnapshotNotFoundErr(localErr) {
		t.Error("local ENOENT on snapshots/ should be not-found")
	}
	denied := errors.New(`rustic forget abc: exit status 1 — path=snapshots/abc123 delete failed PermissionDenied (persistent) at delete => S3Error { code: "AccessDenied" }`)
	if isSnapshotNotFoundErr(denied) {
		t.Error("AccessDenied on snapshots/ must NOT be treated as not-found")
	}
	other := errors.New("rustic prune: exit status 1 — something else broke")
	if isSnapshotNotFoundErr(other) {
		t.Error("unrelated error must not be treated as not-found")
	}
}

func TestForgetSnapshot_AlreadyGone(t *testing.T) {
	writeFakeRustic(t)
	t.Setenv("FAKE_SNAPSHOTS_JSON", `[]`) // repo no longer lists it
	e := New("/tmp/fake.toml")
	err := e.ForgetSnapshot(context.Background(), "f164b918b9a1292434bfacfd129b72d02f741d625b641de00898fe0fd411f96d", false)
	if !errors.Is(err, ErrSnapshotAlreadyGone) {
		t.Fatalf("got %v, want ErrSnapshotAlreadyGone", err)
	}
}

func TestForgetSnapshot_StillListed(t *testing.T) {
	writeFakeRustic(t)
	id := "f164b918b9a1292434bfacfd129b72d02f741d625b641de00898fe0fd411f96d"
	t.Setenv("FAKE_SNAPSHOTS_JSON",
		`[{"group_key": {}, "snapshots": [{"id": "`+id+`", "time": "2026-09-24T00:00:00Z", "hostname": "h", "paths": ["/x"]}]}]`)
	e := New("/tmp/fake.toml")
	err := e.ForgetSnapshot(context.Background(), id, false)
	if errors.Is(err, ErrSnapshotAlreadyGone) {
		t.Fatal("snapshot still listed: must return the real error, not ErrSnapshotAlreadyGone")
	}
	if err == nil || !strings.Contains(err.Error(), "NoSuchKey") {
		t.Fatalf("got %v, want the original not-found error", err)
	}
}
