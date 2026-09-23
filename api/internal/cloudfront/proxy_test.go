package cloudfront

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeCloudFront pretends to be the distribution: it requires the signed-URL
// query params and serves pack bytes for known keys.
func fakeCloudFront(t *testing.T, packs map[string][]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("Expires") == "" || q.Get("Signature") == "" || q.Get("Key-Pair-Id") == "" {
			http.Error(w, "missing signed URL params", http.StatusForbidden)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/")
		data, ok := packs[key]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "11")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Write(data)
	}))
}

func testProxy(t *testing.T, cf *httptest.Server) *Proxy {
	t.Helper()
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	priv, err := ParsePrivateKeyPEM(kp.PrivateKeyPEM)
	if err != nil {
		t.Fatalf("ParsePrivateKeyPEM: %v", err)
	}
	return NewProxy(cf.URL, "cold-bucket", "K2JCJMDEHXQW5F", priv)
}

func TestProxyGET(t *testing.T) {
	cf := fakeCloudFront(t, map[string][]byte{"data/pack1": []byte("pack-bytes!")})
	defer cf.Close()
	proxy := testProxy(t, cf)

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18923/cold-bucket/data/pack1", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	res := rec.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if string(body) != "pack-bytes!" {
		t.Fatalf("body = %q", body)
	}
	if res.Header.Get("ETag") != `"abc123"` {
		t.Fatalf("ETag not passed through: %q", res.Header.Get("ETag"))
	}
}

func TestProxyHEAD(t *testing.T) {
	cf := fakeCloudFront(t, map[string][]byte{"data/pack1": []byte("pack-bytes!")})
	defer cf.Close()
	proxy := testProxy(t, cf)

	req := httptest.NewRequest(http.MethodHead, "http://127.0.0.1:18923/cold-bucket/data/pack1", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	res := rec.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if len(body) != 0 {
		t.Fatalf("HEAD returned a body of %d bytes", len(body))
	}
	if res.Header.Get("Content-Length") == "" {
		t.Fatalf("HEAD missing Content-Length")
	}
}

func TestProxyNotFoundMapsToNoSuchKey(t *testing.T) {
	cf := fakeCloudFront(t, map[string][]byte{})
	defer cf.Close()
	proxy := testProxy(t, cf)

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18923/cold-bucket/data/missing", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	res := rec.Result()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "<Code>NoSuchKey</Code>") {
		t.Fatalf("expected S3 NoSuchKey XML, got: %s", body)
	}
}

func TestProxyRejectsOtherBuckets(t *testing.T) {
	cf := fakeCloudFront(t, map[string][]byte{"data/pack1": []byte("x")})
	defer cf.Close()
	proxy := testProxy(t, cf)

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18923/other-bucket/data/pack1", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Result().StatusCode)
	}
}

func TestProxyRejectsNonReadMethods(t *testing.T) {
	cf := fakeCloudFront(t, map[string][]byte{})
	defer cf.Close()
	proxy := testProxy(t, cf)

	req := httptest.NewRequest(http.MethodPut, "http://127.0.0.1:18923/cold-bucket/data/pack1", strings.NewReader("x"))
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Result().StatusCode)
	}
}

func TestProxyVirtualHostedStyle(t *testing.T) {
	cf := fakeCloudFront(t, map[string][]byte{"data/pack1": []byte("pack-bytes!")})
	defer cf.Close()
	proxy := testProxy(t, cf)

	req := httptest.NewRequest(http.MethodGet, "http://cold-bucket.127.0.0.1:18923/data/pack1", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Result().StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Result().StatusCode)
	}
}

func TestSplitBucketKey(t *testing.T) {
	cases := []struct {
		host, path  string
		bucket, key string
	}{
		{"127.0.0.1:18923", "/cold-bucket/data/p1", "cold-bucket", "data/p1"},
		{"localhost:18923", "/cold-bucket/a", "cold-bucket", "a"},
		{"cold-bucket.example.com", "/data/p1", "cold-bucket", "data/p1"},
		{"127.0.0.1:18923", "/nobucket", "nobucket", ""},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "http://"+c.host+c.path, nil)
		b, k := splitBucketKey(req)
		if b != c.bucket || k != c.key {
			t.Errorf("host=%s path=%s: got (%q,%q), want (%q,%q)", c.host, c.path, b, k, c.bucket, c.key)
		}
	}
}
