package dbadmin

import (
	"strings"
	"testing"
)

func TestEngineForImage(t *testing.T) {
	tests := []struct {
		name  string
		image string
		want  Engine
		match bool
	}{
		// Official images, with and without registry prefixes and tags.
		{"bare postgres", "postgres:16-alpine", EnginePostgres, true},
		{"docker hub library", "docker.io/library/postgres:15", EnginePostgres, true},
		{"private registry", "registry.internal:5000/mycorp/postgres:16", EnginePostgres, true},
		{"digest pinned", "postgres@sha256:abc123", EnginePostgres, true},
		{"postgres variant", "pgvector/pgvector:pg16", EnginePostgres, true},
		{"mysql", "mysql:8.4", EngineMySQL, true},
		{"mariadb", "mariadb:11", EngineMySQL, true},
		{"percona", "percona:8.0", EngineMySQL, true},
		{"mongo", "mongo:7", EngineMongoDB, true},

		// The cases that matter: things that merely mention an engine.
		{"app that mentions postgres in the tag", "mycorp/myapp:postgres-migrator", "", false},
		{"org named after an engine", "postgres-team/myapp:1.0", "", false},
		{"unrelated image", "nginx:1.27", "", false},
		{"empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := EngineForImage(tt.image)
			if ok != tt.match {
				t.Fatalf("matched = %v, want %v (engine %q)", ok, tt.match, got)
			}
			if ok && got != tt.want {
				t.Errorf("engine = %q, want %q", got, tt.want)
			}
		})
	}
}

// A registry port must not be mistaken for an image tag, or the repository
// path is truncated and nothing matches.
func TestImageRepositoryHandlesRegistryPorts(t *testing.T) {
	tests := []struct {
		image string
		want  string
	}{
		{"postgres:16", "postgres"},
		{"registry.internal:5000/postgres", "registry.internal:5000/postgres"},
		{"registry.internal:5000/postgres:16", "registry.internal:5000/postgres"},
		{"postgres@sha256:deadbeef", "postgres"},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			if got := imageRepository(tt.image); got != tt.want {
				t.Errorf("imageRepository(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}

// A password containing "@" or "/" is common and would silently redirect the
// connection to another host if it were not escaped.
func TestDSNEscapesCredentials(t *testing.T) {
	spec, _ := SpecFor(EnginePostgres)

	dsn, err := spec.DSN(Credentials{
		User:     "admin",
		Password: "p@ss/w0rd",
		Database: "appdb",
	}, "127.0.0.1", 5432)
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}

	if strings.Contains(dsn, "p@ss/w0rd") {
		t.Error("password was not escaped; an @ would terminate the userinfo section")
	}
	if !strings.Contains(dsn, "p%40ss%2Fw0rd") {
		t.Errorf("expected escaped password in %q", dsn)
	}
	// The real host must survive escaping.
	if !strings.Contains(dsn, "@127.0.0.1:5432/appdb") {
		t.Errorf("host/database missing or misplaced in %q", dsn)
	}
}

func TestDSNAppliesDefaults(t *testing.T) {
	spec, _ := SpecFor(EnginePostgres)

	dsn, err := spec.DSN(Credentials{Password: "secret"}, "db", 5432)
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}
	if !strings.Contains(dsn, "postgres:secret@") {
		t.Errorf("default user not applied: %q", dsn)
	}
	if !strings.HasSuffix(dsn[:strings.Index(dsn, "?")], "/postgres") {
		t.Errorf("default database not applied: %q", dsn)
	}
}

// Mongo images run unauthenticated unless root credentials were configured.
// Sending empty credentials to such a server fails, so the userless URI is a
// real case rather than a fallback.
func TestMongoDSNOmitsEmptyCredentials(t *testing.T) {
	spec, _ := SpecFor(EngineMongoDB)

	dsn, err := spec.DSN(Credentials{}, "mongo", 27017)
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}
	if strings.Contains(dsn, "@") {
		t.Errorf("expected a credential-free URI, got %q", dsn)
	}

	withAuth, err := spec.DSN(Credentials{User: "root", Password: "pw"}, "mongo", 27017)
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}
	if !strings.Contains(withAuth, "root:pw@mongo:27017") {
		t.Errorf("credentials missing from %q", withAuth)
	}
	// authSource=admin matters: root users live in admin, and omitting it
	// authenticates against the target database and fails.
	if !strings.Contains(withAuth, "authSource=admin") {
		t.Errorf("authSource missing from %q", withAuth)
	}
}

func TestDSNRejectsUnknownEngine(t *testing.T) {
	spec := EngineSpec{Engine: Engine("cassandra")}
	if _, err := spec.DSN(Credentials{User: "u"}, "host", 9042); err == nil {
		t.Error("expected an error for an unsupported engine")
	}
}

func TestEveryEngineHasCompleteSpec(t *testing.T) {
	for _, engine := range []Engine{EnginePostgres, EngineMySQL, EngineMongoDB} {
		spec, ok := SpecFor(engine)
		if !ok {
			t.Fatalf("no spec for %q", engine)
		}
		if spec.DisplayName == "" {
			t.Errorf("%s: missing DisplayName", engine)
		}
		if len(spec.ImageKeywords) == 0 {
			t.Errorf("%s: no image keywords, so it can never be discovered", engine)
		}
		if spec.DefaultPort == 0 {
			t.Errorf("%s: missing DefaultPort", engine)
		}
		// Without these the export/import path has nothing to exec.
		if spec.DumpTool == "" || spec.RestoreTool == "" {
			t.Errorf("%s: missing dump or restore tool", engine)
		}
		if len(spec.PasswordEnv) == 0 {
			t.Errorf("%s: no password env candidates", engine)
		}
	}
}

func TestEnginesListsAll(t *testing.T) {
	if got := len(Engines()); got != 3 {
		t.Errorf("Engines() returned %d specs, want 3", got)
	}
}
