package dbadmin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	queryTimeout   = 30 * time.Second
	defaultLimit   = 50
	maxQueryLimit  = 100
)

// QueryResult is a capped page of rows/documents encoded as JSON objects.
type QueryResult struct {
	Documents []string
	Returned  int32
	Limit     int32
	Skip      int32
	Truncated bool
}

// QueryDocuments fetches a page of rows from a table, or documents from a
// MongoDB collection. limit is clamped to maxQueryLimit; 0 uses the default.
func QueryDocuments(
	ctx context.Context,
	engine Engine,
	creds Credentials,
	host string,
	port int32,
	schema, table string,
	limit, skip int32,
) (*QueryResult, error) {
	if table == "" {
		return nil, fmt.Errorf("table/collection name is required")
	}
	if schema == "" {
		schema = creds.Database
	}
	if schema == "" {
		if spec, ok := SpecFor(engine); ok {
			schema = spec.DefaultDatabase
		}
	}

	limit = clampLimit(limit)
	if skip < 0 {
		skip = 0
	}

	spec, ok := SpecFor(engine)
	if !ok {
		return nil, fmt.Errorf("unsupported engine %q", engine)
	}
	dsn, err := spec.DSN(creds, host, port)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	switch engine {
	case EngineMongoDB:
		return queryMongo(ctx, dsn, schema, table, limit, skip)
	case EnginePostgres:
		return queryPostgres(ctx, dsn, schema, table, limit, skip)
	case EngineMySQL:
		return queryMySQL(ctx, dsn, schema, table, limit, skip)
	default:
		return nil, fmt.Errorf("unsupported engine %q", engine)
	}
}

func clampLimit(limit int32) int32 {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxQueryLimit {
		return maxQueryLimit
	}
	return limit
}

func queryMongo(ctx context.Context, uri, database, collection string, limit, skip int32) (*QueryResult, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = client.Disconnect(context.WithoutCancel(ctx)) }()

	opts := options.Find().
		SetLimit(int64(limit)).
		SetSkip(int64(skip)).
		SetMaxTime(queryTimeout)

	cursor, err := client.Database(database).Collection(collection).Find(ctx, bson.D{}, opts)
	if err != nil {
		return nil, fmt.Errorf("find: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	docs := make([]string, 0, limit)
	for cursor.Next(ctx) {
		var raw bson.M
		if err := cursor.Decode(&raw); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		encoded, err := bsonToJSON(raw)
		if err != nil {
			return nil, err
		}
		docs = append(docs, encoded)
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor: %w", err)
	}

	return &QueryResult{
		Documents: docs,
		Returned:  int32(len(docs)),
		Limit:     limit,
		Skip:      skip,
		Truncated: int32(len(docs)) >= limit,
	}, nil
}

func bsonToJSON(raw bson.M) (string, error) {
	// ExtJSON keeps ObjectId and dates readable without opaque binary blobs.
	bytes, err := bson.MarshalExtJSON(raw, false, false)
	if err != nil {
		// Fallback for values ExtJSON rejects.
		plain, plainErr := json.Marshal(raw)
		if plainErr != nil {
			return "", fmt.Errorf("encode: %w", err)
		}
		return string(plain), nil
	}
	return string(bytes), nil
}

func queryPostgres(ctx context.Context, dsn, schema, table string, limit, skip int32) (*QueryResult, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	// Identifiers cannot be parameterized; quote them after validating.
	schemaIdent, err := quoteSQLIdent(schema)
	if err != nil {
		return nil, err
	}
	tableIdent, err := quoteSQLIdent(table)
	if err != nil {
		return nil, err
	}
	qualified := schemaIdent + "." + tableIdent
	q := fmt.Sprintf(`SELECT row_to_json(t)::text FROM (SELECT * FROM %s LIMIT $1 OFFSET $2) t`, qualified)
	rows, err := conn.Query(ctx, q, limit, skip)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	docs := make([]string, 0, limit)
	for rows.Next() {
		var jsonText string
		if err := rows.Scan(&jsonText); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		docs = append(docs, jsonText)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	return &QueryResult{
		Documents: docs,
		Returned:  int32(len(docs)),
		Limit:     limit,
		Skip:      skip,
		Truncated: int32(len(docs)) >= limit,
	}, nil
}

func queryMySQL(ctx context.Context, dsn, schema, table string, limit, skip int32) (*QueryResult, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	schemaIdent, err := quoteMySQLIdent(schema)
	if err != nil {
		return nil, err
	}
	tableIdent, err := quoteMySQLIdent(table)
	if err != nil {
		return nil, err
	}
	qualified := schemaIdent + "." + tableIdent
	q := fmt.Sprintf("SELECT * FROM %s LIMIT ? OFFSET ?", qualified)
	rows, err := db.QueryContext(ctx, q, limit, skip)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("columns: %w", err)
	}

	docs := make([]string, 0, limit)
	for rows.Next() {
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		obj := make(map[string]any, len(columns))
		for i, col := range columns {
			obj[col] = normalizeSQLValue(values[i])
		}
		encoded, err := json.Marshal(obj)
		if err != nil {
			return nil, fmt.Errorf("encode: %w", err)
		}
		docs = append(docs, string(encoded))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	return &QueryResult{
		Documents: docs,
		Returned:  int32(len(docs)),
		Limit:     limit,
		Skip:      skip,
		Truncated: int32(len(docs)) >= limit,
	}, nil
}

func normalizeSQLValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano)
	default:
		return t
	}
}

// quoteSQLIdent validates and double-quotes a Postgres identifier.
func quoteSQLIdent(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty identifier")
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return "", fmt.Errorf("invalid identifier %q", name)
	}
	return `"` + name + `"`, nil
}

func quoteMySQLIdent(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty identifier")
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return "", fmt.Errorf("invalid identifier %q", name)
	}
	return "`" + name + "`", nil
}
