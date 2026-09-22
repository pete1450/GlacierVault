package engine

import (
	"testing"
)

// Real `rustic snapshots --json` output from rustic v0.11.4 (single group,
// two snapshots; summaries stripped for brevity).
const snapshots0114 = `[{"group_key": {"hostname": "htch-runtime", "label": "", "paths": ["/tmp/rtest/src"]}, "snapshots": [{"time": "2026-09-22T17:26:52.718721159-05:00", "program_version": "rustic v0.9.4", "hostname": "htch-runtime", "tags": [], "paths": ["/tmp/rtest/src"], "id": "e4e5d05fd17984d3fb64b957a2a2805f1a1d3af95111f00fe9d62cd3a2ee8262"}, {"time": "2026-09-22T17:29:00.336208949-05:00", "program_version": "rustic v0.11.4", "hostname": "htch-runtime", "tags": ["test2"], "paths": ["/tmp/rtest/src"], "id": "d2224bc684ed9afb39fdc44ee0ae274468ac67bf2e60926235b47aa2d5503227"}]}]`

// rustic 0.9.x grouped [[group, [snapshots...]]] shape.
const snapshots094 = `[[{"hostname": "h", "paths": ["/src"]}, [{"id": "aaa", "time": "2026-01-01T00:00:00Z", "hostname": "h", "tags": [], "paths": ["/src"]}]]]`

func TestParseSnapshots_Rustic0114(t *testing.T) {
	snaps, err := parseSnapshots([]byte(snapshots0114))
	if err != nil {
		t.Fatalf("parseSnapshots: %v", err)
	}
	if len(snaps) != 2 {
		t.Fatalf("got %d snapshots, want 2", len(snaps))
	}
	if snaps[0].ID != "e4e5d05fd17984d3fb64b957a2a2805f1a1d3af95111f00fe9d62cd3a2ee8262" {
		t.Errorf("snaps[0].ID = %q", snaps[0].ID)
	}
	if snaps[1].Hostname != "htch-runtime" || len(snaps[1].Tags) != 1 || snaps[1].Tags[0] != "test2" {
		t.Errorf("snaps[1] = %+v", snaps[1])
	}
}

func TestParseSnapshots_Rustic094(t *testing.T) {
	snaps, err := parseSnapshots([]byte(snapshots094))
	if err != nil {
		t.Fatalf("parseSnapshots: %v", err)
	}
	if len(snaps) != 1 || snaps[0].ID != "aaa" {
		t.Errorf("got %+v", snaps)
	}
}

func TestParseSnapshots_Flat(t *testing.T) {
	flat := `[{"id": "x", "time": "2026-01-01T00:00:00Z", "hostname": "h"}]`
	snaps, err := parseSnapshots([]byte(flat))
	if err != nil {
		t.Fatalf("parseSnapshots: %v", err)
	}
	if len(snaps) != 1 || snaps[0].ID != "x" {
		t.Errorf("got %+v", snaps)
	}
}
