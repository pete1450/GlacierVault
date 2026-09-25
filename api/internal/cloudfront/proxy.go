package cloudfront

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
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
// URL fetches, so pack downloads ride the free-egress path. It speaks just
// enough of the S3 REST API for restore downloads: path-style GET/HEAD on
// object keys, with S3-style XML errors.
//
// Anything the distribution cannot serve — notably ListObjectsV2, which
// rustic needs to enumerate keys/ and snapshots/ — is passed through to
// real S3, signed with the deployment's AWS credentials. Without that,
// pointing rustic's cold repository at the proxy breaks repository
// operations that list.
type Proxy struct {
	bucket     string // the only bucket served; everything else is denied
	region     string
	accessKey  string
	secretKey  string
	signer     *Signer
	v4signer   *v4.Signer
	httpClient *http.Client
}

// NewProxy builds the translating proxy. baseURL is the CloudFront
// distribution base URL (e.g. https://d111111abcdef8.cloudfront.net);
// privateKey is the URL signing key (kept in memory only). region,
// accessKey and secretKey are the deployment's AWS credentials, used only
// to sign pass-through requests to real S3 (e.g. ListObjectsV2).
func NewProxy(baseURL, bucket, keyPairID string, privateKey *rsa.PrivateKey, region, accessKey, secretKey string) *Proxy {
	return newProxyWithClient(baseURL, bucket, keyPairID, privateKey, region, accessKey, secretKey, http.DefaultClient)
}

func newProxyWithClient(baseURL, bucket, keyPairID string, privateKey *rsa.PrivateKey, region, accessKey, secretKey string, client *http.Client) *Proxy {
	return &Proxy{
		bucket:     bucket,
		region:     region,
		accessKey:  accessKey,
		secretKey:  secretKey,
		signer:     NewSigner(baseURL, keyPairID, privateKey),
		v4signer:   v4.NewSigner(),
		httpClient: client,
	}
}

// ServeHTTP implements the S3 REST surface rustic needs for restores.
// GET/HEAD on /<bucket>/<key> (path style) are served via signed CloudFront
// URLs — the free-egress path. Everything else for the bucket (notably
// ListObjectsV2, which has no key) is passed through to real S3, signed
// with SigV4. Virtual-hosted style (<bucket>.<host>/<key>) is accepted as
// a fallback.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bucket, key := splitBucketKey(r)
	if bucket == "" {
		writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "missing bucket or key")
		return
	}
	if bucket != p.bucket {
		writeS3Error(w, http.StatusForbidden, "AccessDenied", "bucket not served by this proxy")
		return
	}
	if key != "" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		p.serveViaCloudFront(w, r, key)
		return
	}
	p.serveS3Passthrough(w, r, bucket, key)
}

func (p *Proxy) serveViaCloudFront(w http.ResponseWriter, r *http.Request, key string) {
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
	// Forward range/conditional headers: rustic reads packs with ranged
	// GETs (e.g. Range: bytes=0-46). Dropping Range makes CloudFront
	// return the whole object, which rustic rejects as "too much data".
	// (Signed-URL auth covers the URL only, so forwarding headers is safe.)
	for _, h := range []string{"Range", "If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since"} {
		if v := r.Header.Get(h); v != "" {
			upstream.Header.Set(h, v)
		}
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

// serveS3Passthrough forwards requests the distribution cannot serve —
// notably ListObjectsV2 (GET /<bucket>?list-type=2&prefix=...) — to real S3,
// signed with SigV4 using the deployment's credentials. The incoming
// request arrives signed for the proxy endpoint, so its Authorization is
// stripped and the request is re-signed for S3.
func (p *Proxy) serveS3Passthrough(w http.ResponseWriter, r *http.Request, bucket, key string) {
	if p.region == "" || p.accessKey == "" || p.secretKey == "" {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "S3 passthrough not configured")
		return
	}

	upath := "/" + bucket
	if key != "" {
		upath += "/" + key
	}
	target := &url.URL{
		Scheme:   "https",
		Host:     "s3." + p.region + ".amazonaws.com",
		Path:     upath,
		RawQuery: r.URL.RawQuery,
	}

	var bodyBytes []byte
	if r.Body != nil {
		var err error
		if bodyBytes, err = io.ReadAll(r.Body); err != nil {
			writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "could not read request body")
			return
		}
	}
	sum := sha256.Sum256(bodyBytes)
	payloadHash := hex.EncodeToString(sum[:])

	upstream, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "could not build upstream request")
		return
	}
	upstream.ContentLength = int64(len(bodyBytes))
	for h, vals := range r.Header {
		switch http.CanonicalHeaderKey(h) {
		case "Authorization", "X-Amz-Date", "X-Amz-Content-Sha256", "X-Amz-Security-Token",
			"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
			"Te", "Trailer", "Transfer-Encoding", "Upgrade":
			continue
		}
		for _, v := range vals {
			upstream.Header.Add(h, v)
		}
	}

	creds := aws.Credentials{AccessKeyID: p.accessKey, SecretAccessKey: p.secretKey}
	upstream.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if err := p.v4signer.SignHTTP(r.Context(), creds, upstream, payloadHash, "s3", p.region, time.Now()); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "could not sign upstream request")
		return
	}

	resp, err := p.httpClient.Do(upstream)
	if err != nil {
		writeS3Error(w, http.StatusBadGateway, "InternalError", "upstream S3 request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	copyResponseHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, resp.Body)
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
