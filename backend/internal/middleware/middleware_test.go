package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequestIDGeneratesWhenAbsent(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if seen == "" {
		t.Fatal("no request ID on the context")
	}
	if got := rec.Header().Get(RequestIDHeader); got != seen {
		t.Errorf("response header = %q, want %q", got, seen)
	}
}

// One ID must follow a request across hops, so a proxy's value wins.
func TestRequestIDReusesUpstreamValue(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set(RequestIDHeader, "upstream-abc-123")

	h.ServeHTTP(httptest.NewRecorder(), req)

	if seen != "upstream-abc-123" {
		t.Errorf("id = %q, want the upstream value", seen)
	}
}

// The ID is written into logs, so a value containing newlines could forge
// additional log entries. Hostile input must be replaced, not passed through.
func TestRequestIDRejectsHostileInput(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"newline injection", "abc\nlevel=error msg=\"forged entry\""},
		{"carriage return", "abc\r\nX-Injected: yes"},
		{"control characters", "abc\x00\x01"},
		{"over length", strings.Repeat("a", maxInboundIDLength+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen string
			h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen = RequestIDFromContext(r.Context())
			}))

			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			req.Header.Set(RequestIDHeader, tt.value)
			h.ServeHTTP(httptest.NewRecorder(), req)

			if seen == tt.value {
				t.Error("hostile request ID was accepted verbatim")
			}
			if seen == "" {
				t.Error("no replacement ID was generated")
			}
		})
	}
}

func TestRequestIDsAreUnique(t *testing.T) {
	seen := make(map[string]bool, 100)
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := RequestIDFromContext(r.Context())
		if seen[id] {
			t.Errorf("duplicate request ID %q", id)
		}
		seen[id] = true
	}))

	for i := 0; i < 100; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	}
}

func TestCORSEchoesOnlyAllowedOrigins(t *testing.T) {
	h := CORS([]string{"https://idp.example.com", "http://localhost:5173"})(okHandler())

	tests := []struct {
		name       string
		origin     string
		wantHeader string
	}{
		{"allowed production origin", "https://idp.example.com", "https://idp.example.com"},
		{"allowed dev origin", "http://localhost:5173", "http://localhost:5173"},
		{"unlisted origin gets nothing", "https://evil.example.com", ""},
		{"no origin header", "", ""},
		// A near-miss must not pass: substring or suffix matching here would
		// let idp.example.com.evil.com read authenticated responses.
		{"suffix lookalike", "https://idp.example.com.evil.com", ""},
		{"scheme mismatch", "http://idp.example.com", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tt.wantHeader {
				t.Errorf("Allow-Origin = %q, want %q", got, tt.wantHeader)
			}
		})
	}
}

// A disallowed origin must get no credential grant at all.
func TestCORSCredentialsOnlyForAllowedOrigins(t *testing.T) {
	h := CORS([]string{"https://idp.example.com"})(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Allow-Credentials = %q for a disallowed origin", got)
	}
}

func TestCORSPreflightShortCircuits(t *testing.T) {
	reached := false
	h := CORS([]string{"https://idp.example.com"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))

	req := httptest.NewRequest(http.MethodOptions, "/idp.v1.HealthService/Check", nil)
	req.Header.Set("Origin", "https://idp.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if reached {
		t.Error("preflight was passed through to the application handler")
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want Origin; a shared cache could serve one origin's response to another", got)
	}
}

// Cardinality guard: user-controlled path segments must not become labels.
func TestRouteLabelCollapsesDynamicSegments(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/apps/team-a/web", "/apps/*"},
		{"/apps/other-tenant/api", "/apps/*"},
		{"/webhooks/git/3f2b1c-uuid", "/webhooks/git/*"},
		{"/idp.v1.ProjectService/ListProjects", "/idp.v1.ProjectService/ListProjects"},
		{"/api/platform", "/api/platform"},
		{"/healthz", "/healthz"},
		{"/readyz", "/readyz"},
		{"/some/unknown/path", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := routeLabel(tt.path); got != tt.want {
				t.Errorf("routeLabel(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestMetricsHandlerExposesRequestSeries(t *testing.T) {
	h := Metrics(okHandler())
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))

	rec := httptest.NewRecorder()
	MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, MetricsPath, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body, _ := io.ReadAll(rec.Body)
	for _, want := range []string{
		"idp_http_requests_total",
		"idp_http_request_duration_seconds",
		"idp_http_requests_in_flight",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("scrape output is missing %s", want)
		}
	}
}

// Scrapes would otherwise dominate the request count on an idle deployment.
func TestMetricsDoesNotCountItsOwnScrape(t *testing.T) {
	reached := false
	h := Metrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, MetricsPath, nil))

	if !reached {
		t.Error("scrape request was not passed through")
	}

	rec := httptest.NewRecorder()
	MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, MetricsPath, nil))
	body, _ := io.ReadAll(rec.Body)

	if strings.Contains(string(body), `route="`+MetricsPath+`"`) {
		t.Error("the scrape endpoint recorded a series for itself")
	}
}
