package server

import (
	"net/http"
)

// securityHeaders adds defense-in-depth HTTP response headers.
// Must run before the response is written.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		// Prevent MIME sniffing — browsers must respect Content-Type.
		h.Set("X-Content-Type-Options", "nosniff")

		// Block framing (clickjacking). frame-ancestors in CSP covers
		// modern browsers; this covers older ones.
		h.Set("X-Frame-Options", "DENY")

		// Don't leak the full URL in Referer headers.
		h.Set("Referrer-Policy", "same-origin")

		// Disable browser features we don't use.
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")

		// HSTS only makes sense over HTTPS. isSecureRequest checks
		// r.TLS and X-Forwarded-Proto (see cookies.go).
		if isSecureRequest(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		// CSP: lock down to same-origin. 'unsafe-inline' is required for
		// scripts (Next.js static export + theme bootstrap) and styles
		// (Tailwind). React escapes all rendered data, so inline scripts
		// are not an XSS vector here — the risk they mitigate is
		// third-party script injection, which 'self' already blocks.
		// frame-ancestors 'none' is the modern X-Frame-Options.
		// img-src data: allows the TOTP QR code (inline SVG).
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"font-src 'self'; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")

		next.ServeHTTP(w, r)
	})
}
