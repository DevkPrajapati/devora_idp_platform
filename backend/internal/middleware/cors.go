package middleware

import (
	"net/http"
)

// CORS adds Cross-Origin Resource Sharing headers for the configured origins.
//
// The allowlist is matched exactly and the matched value is echoed back, rather
// than reflecting whatever the request sent. Because responses also carry
// Access-Control-Allow-Credentials, reflecting an arbitrary origin would let any
// site on the internet read authenticated responses on a logged-in user's
// behalf. An origin that is not on the list simply gets no CORS headers, and the
// browser blocks the read.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers",
					"Content-Type, Authorization, Connect-Protocol-Version, Connect-Timeout-Ms")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Max-Age", "600")
				// Responses differ by origin, so a shared cache must not serve
				// one origin's response to another.
				w.Header().Add("Vary", "Origin")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
