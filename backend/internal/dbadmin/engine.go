// Package dbadmin inspects and backs up databases running as workloads in the
// cluster.
//
// Databases are discovered from the cluster rather than registered by hand: a
// database is a workload whose container image matches a known engine, and its
// credentials are read from the env the pod already declares. A separate
// registration table would be a second source of truth that drifts the moment
// someone edits a Deployment.
package dbadmin

import (
	"fmt"
	"strings"
)

// Engine identifies a supported database.
type Engine string

const (
	EnginePostgres Engine = "postgres"
	EngineMySQL    Engine = "mysql"
	EngineMongoDB  Engine = "mongodb"
)

// EngineSpec describes how to recognise, reach, and operate one engine.
type EngineSpec struct {
	Engine Engine
	// DisplayName is what the UI shows.
	DisplayName string
	// ImageKeywords match against a container image reference. Matching on the
	// image is what makes discovery work without any annotation on the user's
	// part.
	ImageKeywords []string
	// DefaultPort is used when the Service does not name its port.
	DefaultPort int32
	// UserEnv, PasswordEnv and DatabaseEnv are the environment variables the
	// official images use. They are read from the pod spec and, where the value
	// comes from a secretKeyRef, followed into the Secret.
	UserEnv     []string
	PasswordEnv []string
	DatabaseEnv []string
	// DefaultUser is assumed when the image relies on its built-in default
	// rather than an explicit env var.
	DefaultUser string
	// DefaultDatabase is the database inspected when none is named.
	DefaultDatabase string
	// DumpTool and RestoreTool are the binaries expected inside the database
	// container. Running them there rather than in the platform image is
	// deliberate: dump utilities refuse to work against a server newer than
	// themselves, so the only version guaranteed to match is the one shipped
	// alongside the server.
	DumpTool    string
	RestoreTool string
	// DataDir is where the engine stores durable files. The platform mounts a
	// PVC here so restarts do not wipe tenant data.
	DataDir string
	// ArchiveExt is the download filename suffix for ExportDatabase.
	ArchiveExt string
}

var specs = map[Engine]EngineSpec{
	EnginePostgres: {
		Engine:          EnginePostgres,
		DisplayName:     "PostgreSQL",
		ImageKeywords:   []string{"postgres", "postgresql", "timescaledb", "pgvector"},
		DefaultPort:     5432,
		UserEnv:         []string{"POSTGRES_USER", "PGUSER"},
		PasswordEnv:     []string{"POSTGRES_PASSWORD", "PGPASSWORD"},
		DatabaseEnv:     []string{"POSTGRES_DB", "PGDATABASE"},
		DefaultUser:     "postgres",
		DefaultDatabase: "postgres",
		DumpTool:        "pg_dumpall",
		RestoreTool:     "psql",
		DataDir:         "/var/lib/postgresql/data",
		ArchiveExt:      "sql",
	},
	EngineMySQL: {
		Engine:        EngineMySQL,
		DisplayName:   "MySQL / MariaDB",
		ImageKeywords: []string{"mysql", "mariadb", "percona"},
		DefaultPort:   3306,
		UserEnv:       []string{"MYSQL_USER", "MARIADB_USER"},
		// Root credentials are preferred for inspection because a per-app user
		// usually cannot see other schemas, which would make the table count
		// silently wrong rather than obviously missing.
		PasswordEnv:     []string{"MYSQL_ROOT_PASSWORD", "MARIADB_ROOT_PASSWORD", "MYSQL_PASSWORD", "MARIADB_PASSWORD"},
		DatabaseEnv:     []string{"MYSQL_DATABASE", "MARIADB_DATABASE"},
		DefaultUser:     "root",
		DefaultDatabase: "mysql",
		DumpTool:        "mysqldump",
		RestoreTool:     "mysql",
		DataDir:         "/var/lib/mysql",
		ArchiveExt:      "sql",
	},
	EngineMongoDB: {
		Engine:          EngineMongoDB,
		DisplayName:     "MongoDB",
		ImageKeywords:   []string{"mongo"},
		DefaultPort:     27017,
		UserEnv:         []string{"MONGO_INITDB_ROOT_USERNAME"},
		PasswordEnv:     []string{"MONGO_INITDB_ROOT_PASSWORD"},
		DatabaseEnv:     []string{"MONGO_INITDB_DATABASE"},
		DefaultUser:     "",
		DefaultDatabase: "admin",
		DumpTool:        "mongodump",
		RestoreTool:     "mongorestore",
		DataDir:         "/data/db",
		ArchiveExt:      "archive",
	},
}

// DefaultPVCSize is used when CreateDeployment / EnsurePersistence omit size.
const DefaultPVCSize = "5Gi"

// WorkloadPVCName is the claim attached to a database Deployment.
func WorkloadPVCName(workload string) string {
	return workload + "-data"
}

// DataVolumeName is the volume name used in the pod spec.
const DataVolumeName = "data"

// SpecFor returns the definition for an engine.
func SpecFor(engine Engine) (EngineSpec, bool) {
	spec, ok := specs[engine]
	return spec, ok
}

// Engines returns every supported engine, for the UI's filter list.
func Engines() []EngineSpec {
	out := make([]EngineSpec, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec)
	}
	return out
}

// EngineForImage identifies the engine a container image runs, if any.
//
// Matching is on the final path segment with the tag stripped. Leaving the tag
// in would let "myapp:postgres-migrator" register as a Postgres server, and
// checking only the last segment stops "postgres-team/myapp" from matching
// while still allowing "mycorp/postgres".
func EngineForImage(image string) (Engine, bool) {
	repo := strings.ToLower(imageRepository(image))
	if repo == "" {
		return "", false
	}

	name := repo
	if idx := strings.LastIndex(repo, "/"); idx >= 0 {
		name = repo[idx+1:]
	}

	// Iterated in a fixed order so an image matching two keyword sets resolves
	// the same way on every call; Go map iteration order is randomised.
	for _, engine := range []Engine{EnginePostgres, EngineMySQL, EngineMongoDB} {
		for _, keyword := range specs[engine].ImageKeywords {
			if name == keyword ||
				strings.HasPrefix(name, keyword+"-") ||
				strings.HasPrefix(name, keyword+"_") {
				return engine, true
			}
		}
	}
	return "", false
}

// imageRepository strips the tag or digest from an image reference.
func imageRepository(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}

	// A digest always wins over a tag.
	if idx := strings.Index(image, "@"); idx >= 0 {
		image = image[:idx]
	}

	// A colon before the last slash belongs to a registry port, not a tag.
	lastColon := strings.LastIndex(image, ":")
	lastSlash := strings.LastIndex(image, "/")
	if lastColon > lastSlash {
		image = image[:lastColon]
	}

	return image
}

// Credentials are what the platform needs to reach one database.
type Credentials struct {
	User     string
	Password string
	Database string
}

// DSN renders a connection string for the engine, given a reachable endpoint.
//
// The caller supplies host and port because the address depends on whether the
// platform is running inside the cluster or tunnelling through a port-forward.
func (s EngineSpec) DSN(creds Credentials, host string, port int32) (string, error) {
	user := creds.User
	if user == "" {
		user = s.DefaultUser
	}
	database := creds.Database
	if database == "" {
		database = s.DefaultDatabase
	}

	switch s.Engine {
	case EnginePostgres:
		if user == "" {
			return "", fmt.Errorf("postgres requires a user")
		}
		// sslmode=disable because the connection is either inside the cluster
		// network or through a local port-forward; in both cases TLS would be
		// terminated at an endpoint the server has no certificate for.
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable&connect_timeout=10",
			urlEscape(user), urlEscape(creds.Password), host, port, database), nil

	case EngineMySQL:
		if user == "" {
			return "", fmt.Errorf("mysql requires a user")
		}
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?timeout=10s&readTimeout=30s",
			user, creds.Password, host, port, database), nil

	case EngineMongoDB:
		// Mongo images run without auth unless root credentials were set, and
		// sending empty credentials to an unauthenticated server fails outright
		// — so the userless form is a real case, not a fallback.
		if user == "" {
			return fmt.Sprintf("mongodb://%s:%d/?connectTimeoutMS=10000", host, port), nil
		}
		return fmt.Sprintf("mongodb://%s:%s@%s:%d/?authSource=admin&connectTimeoutMS=10000",
			urlEscape(user), urlEscape(creds.Password), host, port), nil

	default:
		return "", fmt.Errorf("unsupported engine %q", s.Engine)
	}
}

// urlEscape percent-encodes the characters that would otherwise terminate the
// userinfo section of a URI. A password containing "@" or "/" is common and
// would silently produce a connection to the wrong host without this.
func urlEscape(s string) string {
	replacer := strings.NewReplacer(
		"%", "%25",
		"@", "%40",
		":", "%3A",
		"/", "%2F",
		"?", "%3F",
		"#", "%23",
		"[", "%5B",
		"]", "%5D",
	)
	return replacer.Replace(s)
}
