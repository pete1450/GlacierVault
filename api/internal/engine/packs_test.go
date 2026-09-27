package engine

import "testing"

// Mirrors the real `rustic cat index <ID>` shape (single {"packs": [...]}
// object), verified against rustic 0.11.4.
func TestAddDataPacks(t *testing.T) {
	doc := []byte(`{"packs": [
		{"id": "aaa", "blobs": [
			{"id": "b1", "type": "data", "offset": 0, "length": 10},
			{"id": "b2", "type": "data", "offset": 10, "length": 10}
		], "time": "2026-09-27T09:03:38Z"},
		{"id": "bbb", "blobs": [
			{"id": "t1", "type": "tree", "offset": 0, "length": 5}
		], "time": "2026-09-27T09:03:38Z"}
	]}`)
	seen := map[string]bool{}
	if err := addDataPacks(doc, seen); err != nil {
		t.Fatal(err)
	}
	// aaa has data blobs; bbb is tree-only and excluded.
	if len(seen) != 1 || !seen["aaa"] {
		t.Fatalf("seen=%v, want {aaa}", seen)
	}
}

func TestAddDataPacks_DedupesAcrossIndexes(t *testing.T) {
	doc1 := []byte(`{"packs": [{"id": "aaa", "blobs": [{"id": "b1", "type": "data"}]}]}`)
	doc2 := []byte(`{"packs": [
		{"id": "aaa", "blobs": [{"id": "b1", "type": "data"}]},
		{"id": "ccc", "blobs": [{"id": "b3", "type": "data"}]}
	]}`)
	seen := map[string]bool{}
	for _, doc := range [][]byte{doc1, doc2} {
		if err := addDataPacks(doc, seen); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 2 || !seen["aaa"] || !seen["ccc"] {
		t.Fatalf("seen=%v, want {aaa ccc}", seen)
	}
}

func TestAddDataPacks_Empty(t *testing.T) {
	seen := map[string]bool{}
	if err := addDataPacks([]byte(`{"packs":[]}`), seen); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 0 {
		t.Fatalf("seen=%v, want empty", seen)
	}
}

func TestAddDataPacks_BadJSON(t *testing.T) {
	if err := addDataPacks([]byte(`not json`), map[string]bool{}); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseIndexIDs(t *testing.T) {
	out := []byte("IndexId(17c07e5058f635bfeac643d51fe06883d10939df63ad7b7ea27ff4655ce40373)\n" +
		"IndexId(aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa)\n" +
		"some junk line\n")
	ids := parseIndexIDs(out)
	if len(ids) != 2 {
		t.Fatalf("ids=%v, want 2", ids)
	}
	if ids[0] != "17c07e5058f635bfeac643d51fe06883d10939df63ad7b7ea27ff4655ce40373" {
		t.Fatalf("ids[0]=%q, want bare hex id", ids[0])
	}
}

func TestParseIndexIDs_Empty(t *testing.T) {
	if ids := parseIndexIDs([]byte("")); len(ids) != 0 {
		t.Fatalf("ids=%v, want empty", ids)
	}
}
