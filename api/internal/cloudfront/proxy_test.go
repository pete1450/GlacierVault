package cloudfront

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	return NewProxy(cf.URL, "cold-bucket", "K2JCJMDEHXQW5F", priv, "us-east-1", "test-access-key", "test-secret-key")
}

// rewriteTransport redirects requests to the fake S3 server while keeping
// the original (S3) URL the signature was computed for.
type rewriteTransport struct {
	target string // host:port of the fake server
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = rt.target
	req2.Host = rt.target
	return http.DefaultTransport.RoundTrip(req2)
}

// fakeS3 pretends to be real S3 for pass-through requests: it requires a
// SigV4 Authorization header for the test credentials and serves a canned
// ListObjectsV2 response.
func fakeS3(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=test-access-key/") ||
			!strings.Contains(auth, "/us-east-1/s3/aws4_request") {
			http.Error(w, "bad signature: "+auth, http.StatusForbidden)
			return
		}
		if r.Header.Get("X-Amz-Date") == "" || r.Header.Get("X-Amz-Content-Sha256") == "" {
			http.Error(w, "missing Amz headers", http.StatusForbidden)
			return
		}
		if r.URL.Query().Get("list-type") == "2" {
			w.Header().Set("Content-Type", "application/xml")
			io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+
				`<ListBucketResult><Contents><Key>keys/abc123</Key></Contents></ListBucketResult>`)
			return
		}
		io.WriteString(w, "s3-passthrough-ok")
	}))
}

func testProxyWithS3(t *testing.T, cf, s3srv *httptest.Server) *Proxy {
	t.Helper()
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	priv, err := ParsePrivateKeyPEM(kp.PrivateKeyPEM)
	if err != nil {
		t.Fatalf("ParsePrivateKeyPEM: %v", err)
	}
	u, err := url.Parse(s3srv.URL)
	if err != nil {
		t.Fatalf("parse fake s3 url: %v", err)
	}
	client := &http.Client{Transport: &rewriteTransport{target: u.Host}}
	return newProxyWithClient(cf.URL, "cold-bucket", "K2JCJMDEHXQW5F", priv,
		"us-east-1", "test-access-key", "test-secret-key", client)
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

func TestProxyNonReadMethodPassthrough(t *testing.T) {
	cf := fakeCloudFront(t, map[string][]byte{})
	defer cf.Close()
	s3srv := fakeS3(t)
	defer s3srv.Close()
	proxy := testProxyWithS3(t, cf, s3srv)

	// Non-GET/HEAD requests can't be served by the distribution; they are
	// passed through to real S3 instead of rejected.
	req := httptest.NewRequest(http.MethodPut, "http://127.0.0.1:18923/cold-bucket/data/pack1", strings.NewReader("x"))
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	res := rec.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if string(body) != "s3-passthrough-ok" {
		t.Fatalf("body = %q", body)
	}
}

func TestProxyListObjectsPassthrough(t *testing.T) {
	cf := fakeCloudFront(t, map[string][]byte{})
	defer cf.Close()
	s3srv := fakeS3(t)
	defer s3srv.Close()
	proxy := testProxyWithS3(t, cf, s3srv)

	// This is the exact call rustic makes against the cold repo that was
	// failing with "missing bucket or key" before the passthrough existed.
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18923/cold-bucket?list-type=2&prefix=keys%2F&encoding-type=url", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	res := rec.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "<Key>keys/abc123</Key>") {
		t.Fatalf("expected ListBucketResult XML, got: %s", body)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/xml" {
		t.Fatalf("Content-Type = %q, want application/xml", ct)
	}
}

func TestProxyForwardsRangeHeader(t *testing.T) {
	// Rustic reads packs with ranged GETs; the proxy must forward the
	// Range header or CloudFront returns the whole object and rustic
	// fails with "reader got too much data".
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("Signature") == "" {
			http.Error(w, "unsigned", http.StatusForbidden)
			return
		}
		if got := r.Header.Get("Range"); got != "bytes=0-46" {
			http.Error(w, "Range not forwarded, got: "+got, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Range", "bytes 0-46/124")
		w.Header().Set("Content-Length", "47")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(make([]byte, 47))
	}))
	defer cf.Close()
	proxy := testProxy(t, cf)

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18923/cold-bucket/data/41/4135db6a", nil)
	req.Header.Set("Range", "bytes=0-46")
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	res := rec.Result()
	if res.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if len(body) != 47 {
		t.Fatalf("body length = %d, want 47", len(body))
	}
	if cr := res.Header.Get("Content-Range"); cr != "bytes 0-46/124" {
		t.Fatalf("Content-Range = %q", cr)
	}
}

func TestProxyPassthroughRejectsOtherBuckets(t *testing.T) {
	cf := fakeCloudFront(t, map[string][]byte{})
	defer cf.Close()
	s3srv := fakeS3(t)
	defer s3srv.Close()
	proxy := testProxyWithS3(t, cf, s3srv)

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18923/other-bucket?list-type=2", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Result().StatusCode)
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
