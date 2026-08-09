package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// RequestIDHeader is the header carrying the correlation ID, both inbound and
// outbound. X-Request-Id is the de facto standard that ingress controllers and
// load balancers already populate.
const RequestIDHeader = "X-Request-Id"

type requestIDKey struct{}

// maxInboundIDLength bounds what is accepted from a caller. An unbounded value
// would be echoed into every log line for that request, letting a client inflate
// log volume at will.
const maxInboundIDLength = 128

// RequestID attaches a correlation ID to each request.
//
// An upstream proxy's ID is reused when present so one identifier follows a
// request across every hop; otherwise a fresh one is generated. The value is
// echoed back on the response so a user reporting a failure can quote the exact
// ID to grep for.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" || len(id) > maxInboundIDLength || !isPrintableASCII(id) {
			// Rejecting non-printable input matters: the ID lands in logs, and
			// a value containing newlines could forge additional log entries.
			id = newRequestID()
		}

		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(ContextWithRequestID(r.Context(), id)))
	})
}

// ContextWithRequestID stores a correlation ID on the context.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFromContext returns the correlation ID, or "" when unset.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// newRequestID returns 16 random hex bytes.
//
// Not a UUID: this only has to be unique enough to correlate log lines within a
// retention window, and avoiding the dependency keeps the module graph smaller.
// crypto/rand rather than math/rand so IDs are not predictable — a guessable ID
// would let a caller collide with another request's logs deliberately.
func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// rand.Read on a healthy system does not fail. If it somehow does, an
		// empty ID beats refusing to serve the request.
		return ""
	}
	return hex.EncodeToString(buf)
}

func isPrintableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}
