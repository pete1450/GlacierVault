package engine

import "testing"

func TestCountDataPacks(t *testing.T) {
	out := []byte(`[
		{"packs": [
			{"id": "aaa", "blobs": [
				{"id": "b1", "type": "data", "offset": 0, "length": 10},
				{"id": "b2", "type": "data", "offset": 10, "length": 10}
			]},
			{"id": "bbb", "blobs": [
				{"id": "t1", "type": "tree", "offset": 0, "length": 5}
			]}
		]},
		{"packs": [
			{"id": "aaa", "blobs": [
				{"id": "b1", "type": "data", "offset": 0, "length": 10}
			]},
			{"id": "ccc", "blobs": [
				{"id": "b3", "type": "data", "offset": 0, "length": 7}
			]}
		]}
	]`)
	n, err := countDataPacks(out)
	if err != nil {
		t.Fatal(err)
	}
	// aaa and ccc have data blobs (aaa twice — deduped); bbb is tree-only.
	if n != 2 {
		t.Fatalf("packs=%d, want 2", n)
	}
}

func TestCountDataPacks_Empty(t *testing.T) {
	n, err := countDataPacks([]byte(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("packs=%d, want 0", n)
	}
}

func TestCountDataPacks_BadJSON(t *testing.T) {
	if _, err := countDataPacks([]byte(`not json`)); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
