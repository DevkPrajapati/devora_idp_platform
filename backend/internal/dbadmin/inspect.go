package dbadmin

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	_ "github.com/go-sql-driver/mysql" // registers the "mysql" driver
	"github.com/jackc/pgx/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// inspectTimeout bounds a whole inspection. A database under heavy load can
// make catalogue queries slow, and an unbounded request would hold a
// port-forward open indefinitely.
const inspectTimeout = 30 * time.Second

// maxTablesReturned caps the per-table detail list. A schema with 50,000
// tables would otherwise produce a response no browser can render; the count
// stays exact regardless.
const maxTablesReturned = 500

// Table is one table, or one collection on MongoDB.
type Table struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	// RowEstimate is an approximation on every engine. Postgres reports
	// planner statistics and MySQL's InnoDB estimate can be off by orders of
	// magnitude — an exact COUNT(*) per table would scan the entire database.
	RowEstimate int64 `json:"rowEstimate"`
	SizeBytes   int64 `json:"sizeBytes"`
}

// Overview is the result of inspecting one database.
type Overview struct {
	Engine   Engine `json:"engine"`
	Database string `json:"database"`
	// Version is the server's own version string, which is also what decides
	// whether a dump taken here can be restored elsewhere.
	Version string `json:"version"`
	// TableCount is exact even when the Tables list is truncated.
	TableCount int32 `json:"tableCount"`
	// SchemaCount is the number of distinct non-system schemas, or databases
	// on MongoDB.
	SchemaCount int32 `json:"schemaCount"`
	SizeBytes   int64 `json:"sizeBytes"`
	// ActiveConnections is -1 where the engine does not report it.
	ActiveConnections int32 `json:"activeConnections"`
	// Tables is capped at maxTablesReturned, largest first.
	Tables []Table `json:"tables"`
	// TablesTruncated tells the UI to say "showing 500 of N".
	TablesTruncated bool      `json:"tablesTruncated"`
	InspectedAt     time.Time `json:"inspectedAt"`
}

// Inspect connects to a database and reports its structure.
//
// host and port must already be reachable — the caller decides whether that
// means in-cluster DNS or a local port-forward.
func Inspect(
	ctx context.Context,
	engine Engine,
	creds Credentials,
	host string,
	port int32,
) (*Overview, error) {
	spec, ok := SpecFor(engine)
	if !ok {
		return nil, fmt.Errorf("unsupported engine %q", engine)
	}

	dsn, err := spec.DSN(creds, host, port)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()

	switch engine {
	case EnginePostgres:
		return inspectPostgres(ctx, dsn, creds)
	case EngineMySQL:
		return inspectMySQL(ctx, dsn, creds)
	case EngineMongoDB:
		return inspectMongo(ctx, dsn, creds)
	default:
		return nil, fmt.Errorf("unsupported engine %q", engine)
	}
}

func inspectPostgres(ctx context.Context, dsn string, creds Credentials) (*Overview, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	overview := &Overview{
		Engine:            EnginePostgres,
		Database:          creds.Database,
		ActiveConnections: -1,
		Tables:            make([]Table, 0),
		InspectedAt:       time.Now().UTC(),
	}

	_ = conn.QueryRow(ctx, `SELECT version()`).Scan(&overview.Version)
	_ = conn.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&overview.SizeBytes)
	_ = conn.QueryRow(ctx,
		`SELECT count(*)::int FROM pg_stat_activity WHERE datname = current_database()`,
	).Scan(&overview.ActiveConnections)

	// Excluding pg_catalog and information_schema is what makes this "the
	// user's tables" rather than "roughly 200 system tables plus yours".
	// pg_toast_* is excluded too: TOAST tables are storage internals, not
	// objects anyone created.
	const countQuery = `
		SELECT count(*)::int, count(DISTINCT table_schema)::int
		FROM information_schema.tables
		WHERE table_type = 'BASE TABLE'
		  AND table_schema NOT IN ('pg_catalog', 'information_schema')
		  AND table_schema NOT LIKE 'pg_toast%'`
	if err := conn.QueryRow(ctx, countQuery).Scan(&overview.TableCount, &overview.SchemaCount); err != nil {
		return nil, fmt.Errorf("count tables: %w", err)
	}

	// reltuples is the planner's estimate and reads -1 on a table that has
	// never been analysed; clamping to 0 avoids showing a negative row count.
	const tableQuery = `
		SELECT n.nspname,
		       c.relname,
		       GREATEST(c.reltuples, 0)::bigint,
		       pg_total_relation_size(c.oid)::bigint
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'r'
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		  AND n.nspname NOT LIKE 'pg_toast%'
		ORDER BY pg_total_relation_size(c.oid) DESC
		LIMIT $1`

	rows, err := conn.Query(ctx, tableQuery, maxTablesReturned)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Schema, &t.Name, &t.RowEstimate, &t.SizeBytes); err != nil {
			return nil, fmt.Errorf("scan table: %w", err)
		}
		overview.Tables = append(overview.Tables, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read tables: %w", err)
	}

	overview.TablesTruncated = int(overview.TableCount) > len(overview.Tables)
	return overview, nil
}

func inspectMySQL(ctx context.Context, dsn string, creds Credentials) (*Overview, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	overview := &Overview{
		Engine:            EngineMySQL,
		Database:          creds.Database,
		ActiveConnections: -1,
		Tables:            make([]Table, 0),
		InspectedAt:       time.Now().UTC(),
	}

	_ = db.QueryRowContext(ctx, `SELECT VERSION()`).Scan(&overview.Version)

	var threads sql.NullInt64
	if err := db.QueryRowContext(ctx,
		`SELECT VARIABLE_VALUE FROM performance_schema.global_status
		 WHERE VARIABLE_NAME = 'THREADS_CONNECTED'`).Scan(&threads); err == nil && threads.Valid {
		overview.ActiveConnections = int32(threads.Int64)
	}

	// MySQL's "schema" and "database" are the same thing, so the system
	// schemas below are the equivalent of Postgres's pg_catalog.
	const systemSchemas = `('mysql', 'information_schema', 'performance_schema', 'sys')`

	const countQuery = `
		SELECT COUNT(*), COUNT(DISTINCT TABLE_SCHEMA)
		FROM information_schema.TABLES
		WHERE TABLE_TYPE = 'BASE TABLE'
		  AND TABLE_SCHEMA NOT IN ` + systemSchemas
	if err := db.QueryRowContext(ctx, countQuery).Scan(&overview.TableCount, &overview.SchemaCount); err != nil {
		return nil, fmt.Errorf("count tables: %w", err)
	}

	const sizeQuery = `
		SELECT COALESCE(SUM(DATA_LENGTH + INDEX_LENGTH), 0)
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA NOT IN ` + systemSchemas
	_ = db.QueryRowContext(ctx, sizeQuery).Scan(&overview.SizeBytes)

	const tableQuery = `
		SELECT TABLE_SCHEMA,
		       TABLE_NAME,
		       COALESCE(TABLE_ROWS, 0),
		       COALESCE(DATA_LENGTH + INDEX_LENGTH, 0)
		FROM information_schema.TABLES
		WHERE TABLE_TYPE = 'BASE TABLE'
		  AND TABLE_SCHEMA NOT IN ` + systemSchemas + `
		ORDER BY (DATA_LENGTH + INDEX_LENGTH) DESC
		LIMIT ?`

	rows, err := db.QueryContext(ctx, tableQuery, maxTablesReturned)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Schema, &t.Name, &t.RowEstimate, &t.SizeBytes); err != nil {
			return nil, fmt.Errorf("scan table: %w", err)
		}
		overview.Tables = append(overview.Tables, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read tables: %w", err)
	}

	overview.TablesTruncated = int(overview.TableCount) > len(overview.Tables)
	return overview, nil
}

func inspectMongo(ctx context.Context, uri string, creds Credentials) (*Overview, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	// WithoutCancel so disconnect still runs cleanly when the inspection
	// deadline is what ended the call.
	defer func() { _ = client.Disconnect(context.WithoutCancel(ctx)) }()

	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	overview := &Overview{
		Engine:            EngineMongoDB,
		Database:          creds.Database,
		ActiveConnections: -1,
		Tables:            make([]Table, 0),
		InspectedAt:       time.Now().UTC(),
	}

	var buildInfo struct {
		Version string `bson:"version"`
	}
	_ = client.Database("admin").RunCommand(ctx, bson.D{{Key: "buildInfo", Value: 1}}).Decode(&buildInfo)
	overview.Version = buildInfo.Version

	var serverStatus struct {
		Connections struct {
			Current int32 `bson:"current"`
		} `bson:"connections"`
	}
	if err := client.Database("admin").
		RunCommand(ctx, bson.D{{Key: "serverStatus", Value: 1}}).
		Decode(&serverStatus); err == nil {
		overview.ActiveConnections = serverStatus.Connections.Current
	}

	// Mongo has no tables; collections are the closest equivalent and are what
	// the user is counting. System databases are skipped for the same reason
	// pg_catalog is on Postgres.
	names, err := client.ListDatabaseNames(ctx, bson.D{})
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}

	systemDatabases := map[string]bool{"admin": true, "local": true, "config": true}
	for _, dbName := range names {
		if systemDatabases[dbName] {
			continue
		}
		overview.SchemaCount++

		database := client.Database(dbName)
		collections, err := database.ListCollectionNames(ctx, bson.D{})
		if err != nil {
			// One unreadable database should not fail the whole inspection —
			// the caller may simply lack rights on it.
			continue
		}

		for _, collection := range collections {
			overview.TableCount++

			var stats struct {
				Count      int64 `bson:"count"`
				Size       int64 `bson:"size"`
				TotalIndex int64 `bson:"totalIndexSize"`
			}
			if err := database.RunCommand(ctx, bson.D{
				{Key: "collStats", Value: collection},
			}).Decode(&stats); err != nil {
				// Still counted above; only the detail row is lost.
				continue
			}

			overview.SizeBytes += stats.Size + stats.TotalIndex
			overview.Tables = append(overview.Tables, Table{
				Schema:      dbName,
				Name:        collection,
				RowEstimate: stats.Count,
				SizeBytes:   stats.Size + stats.TotalIndex,
			})
		}
	}

	// Sorted then capped so truncation drops the smallest collections rather
	// than whichever the server happened to list last.
	sort.Slice(overview.Tables, func(i, j int) bool {
		return overview.Tables[i].SizeBytes > overview.Tables[j].SizeBytes
	})
	if len(overview.Tables) > maxTablesReturned {
		overview.Tables = overview.Tables[:maxTablesReturned]
		overview.TablesTruncated = true
	}

	return overview, nil
}
