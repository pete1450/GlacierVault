package engine

import "testing"

func TestParseRepoInfoJSON_ArrayShape(t *testing.T) {
	out := []byte(`{"files": [
		{"type": "key", "count": 1, "size": 363},
		{"type": "snapshot", "count": 12, "size": 6041},
		{"type": "index", "count": 3, "size": 5734},
		{"type": "pack", "count": 5, "size": 54001664},
		{"type": "total", "count": 21, "size": 54013702}
	]}`)
	info, ok := parseRepoInfoJSON(out)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if info.TotalBytes != 54013702 {
		t.Fatalf("total=%d", info.TotalBytes)
	}
	if info.PackBytes != 54001664 {
		t.Fatalf("pack=%d", info.PackBytes)
	}
	if info.SnapshotCount != 12 {
		t.Fatalf("snapshots=%d", info.SnapshotCount)
	}
}

func TestParseRepoInfoJSON_Rustic094Shape(t *testing.T) {
	// Actual `rustic repoinfo --json` output from rustic 0.9.4: files.repo
	// is an array of {tpe,count,size}; there is no "total" entry.
	out := []byte(`{"files": {"repo": [
		{"tpe": "key", "count": 1, "size": 363},
		{"tpe": "snapshot", "count": 12, "size": 6041},
		{"tpe": "index", "count": 3, "size": 5734},
		{"tpe": "pack", "count": 5, "size": 54001664}
	]}, "index": {}}`)
	info, ok := parseRepoInfoJSON(out)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if info.TotalBytes != 54001664+5734+6041+363 {
		t.Fatalf("total=%d", info.TotalBytes)
	}
	if info.PackBytes != 54001664 {
		t.Fatalf("pack=%d", info.PackBytes)
	}
	if info.SnapshotCount != 12 {
		t.Fatalf("snapshots=%d", info.SnapshotCount)
	}
}

func TestParseRepoInfoJSON_ObjectShape(t *testing.T) {
	out := []byte(`{"files": {
		"key": {"count": 1, "total_size": 363},
		"pack": {"count": 5, "total_size": 54001664},
		"index": {"count": 3, "total_size": 5734},
		"snapshot": {"count": 12, "total_size": 6041}
	}}`)
	info, ok := parseRepoInfoJSON(out)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// No "total" entry; total is the sum of parts.
	if info.TotalBytes != 54001664+5734+6041+363 {
		t.Fatalf("total=%d", info.TotalBytes)
	}
	if info.SnapshotCount != 12 {
		t.Fatalf("snapshots=%d", info.SnapshotCount)
	}
}

func TestParseRepoInfoText(t *testing.T) {
	out := []byte(`| File type | Count | Total Size |
|-----------|-------|------------|
| Key       |     1 |      363 B |
| Snapshot  |    12 |    5.9 kiB |
| Index     |     3 |    5.6 kiB |
| Pack      |     5 |   51.5 MiB |
| Total     |    21 |   51.5 MiB |
`)
	info, err := parseRepoInfoText(out)
	if err != nil {
		t.Fatal(err)
	}
	want := int64(54001664)
	if info.TotalBytes != want {
		t.Fatalf("total=%d want %d", info.TotalBytes, want)
	}
	if info.PackBytes != want {
		t.Fatalf("pack=%d want %d", info.PackBytes, want)
	}
	if info.SnapshotCount != 12 {
		t.Fatalf("snapshots=%d", info.SnapshotCount)
	}
}

func TestParseHumanSize(t *testing.T) {
	cases := map[string]int64{
		"363 B":    363,
		"5.9 kiB":  6041,
		"51.5 MiB": 54001664,
		"1.2 GiB":  1288490188,
		"2 TiB":    2 * 1024 * 1024 * 1024 * 1024,
	}
	for in, want := range cases {
		got, err := parseHumanSize(in)
		if err != nil || got != want {
			t.Fatalf("%q: got %d, err %v; want %d", in, got, err, want)
		}
	}
}
