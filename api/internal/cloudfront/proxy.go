package cloudfront

import (
	"crypto/rsa"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// DefaultAddr is the localhost address the S3→CloudFront proxy listens on.
// It is deliberately loopback-only: the proxy translates rustic's S3
// requests into signed CloudFront URLs, and must never be reachable from
// outside the container.
const DefaultAddr = "127.0.0.1:18923"

// URLValidity is how long each minted signed URL stays valid. Short on
// purpose: URLs never leave the container, and a minute-scale expiry bounds
// the blast radius of any leak.
const URLValidity = 2 * time.Minute

// Proxy translates S3 GET/HEAD requests from rustic into CloudFront signed
// URL fetches. It speaks just enough of the S3 REST API for restore
// downloads: path-style GET/HEAD on object keys, with S3-style XML errors.
type Proxy struct {
	bucket     string // the only bucket served; everything else is denied
	signer     *Signer
	httpClient *http.Client
}

// NewProxy builds the translating proxy. baseURL is the CloudFront
// distribution base URL (e.g. https://d111111abcdef8.cloudfront.net);
// privateKey is the URL signing key (kept in memory only).
func NewProxy(baseURL, bucket, keyPairID string, privateKey *rsa.PrivateKey) *Proxy {
	return newProxyWithClient(baseURL, bucket, keyPairID, privateKey, http.DefaultClient)
}

func newProxyWithClient(baseURL, bucket, keyPairID string, privateKey *rsa.PrivateKey, client *http.Client) *Proxy {
	return &Proxy{
		bucket:     bucket,
		signer:     NewSigner(baseURL, keyPairID, privateKey),
		httpClient: client,
	}
}

// ServeHTTP implements the minimal S3 REST surface rustic needs for restore
// downloads: GET and HEAD on /<bucket>/<key> (path style). Virtual-hosted
// style (<bucket>.<host>/<key>) is accepted as a fallback.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeS3Error(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "only GET and HEAD are supported")
		return
	}
	bucket, key := splitBucketKey(r)
	if bucket == "" || key == "" {
		writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "missing bucket or key")
		return
	}
	if bucket != p.bucket {
		writeS3Error(w, http.StatusForbidden, "AccessDenied", "bucket not served by this proxy")
		return
	}

	signed, err := p.signer.SignURL(key, time.Now().Add(URLValidity))
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "could not sign URL")
		return
	}

	upstream, err := http.NewRequestWithContext(r.Context(), r.Method, signed, nil)
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "could not build upstream request")
		return
	}
	resp, err := p.httpClient.Do(upstream)
	if err != nil {
		writeS3Error(w, http.StatusBadGateway, "InternalError", "upstream fetch failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		copyResponseHeaders(w, resp)
		w.WriteHeader(resp.StatusCode)
		if r.Method == http.MethodGet {
			_, _ = io.Copy(w, resp.Body)
		}
	case resp.StatusCode == http.StatusNotFound:
		writeS3Error(w, http.StatusNotFound, "NoSuchKey", "the specified key does not exist")
	case resp.StatusCode == http.StatusForbidden:
		writeS3Error(w, http.StatusForbidden, "AccessDenied", "access denied")
	case resp.StatusCode >= 500:
		// 5xx from CloudFront/S3: surface as a retryable S3 error.
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "upstream error")
	default:
		writeS3Error(w, resp.StatusCode, "InternalError", "unexpected upstream status")
	}
}

// splitBucketKey extracts bucket and key from path-style (/b/k) or
// virtual-hosted-style (b.host/k) requests.
func splitBucketKey(r *http.Request) (bucket, key string) {
	if host := r.Host; host != "" {
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if strings.HasPrefix(host, "s3.") || strings.HasPrefix(host, "127.") || host == "localhost" {
			// Not virtual-hosted style; fall through to path parsing.
		} else if i := strings.Index(host, "."); i > 0 {
			return host[:i], strings.TrimPrefix(r.URL.Path, "/")
		}
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/")
	i := strings.Index(trimmed, "/")
	if i < 0 {
		return trimmed, ""
	}
	return trimmed[:i], trimmed[i+1:]
}

// copyResponseHeaders passes through the headers rustic/opendal cares about.
func copyResponseHeaders(w http.ResponseWriter, resp *http.Response) {
	for _, h := range []string{"Content-Length", "Content-Type", "ETag", "Last-Modified", "Accept-Ranges", "Content-Range"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
}

// writeS3Error renders an S3-style XML error document.
func writeS3Error(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>`+
		`<Error><Code>%s</Code><Message>%s</Message></Error>`, code, message)
}

// Reachable reports whether the proxy is listening at addr.
func Reachable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
