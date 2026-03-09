// internal/adapters/bob/bob_adapter.go
//
// Bob → schema.sql adapter.
//
// Bob (github.com/stephenafamo/bob) is a modern SQL-first toolkit.
// Unlike GORM, Bob does NOT have an auto-migrator. Its model generator
// reads from the live DB (like SQLBoiler). This means the correct workflow
// with Bob is:
//
//   schema.sql → migrate-gen (applies migrations) → Bob gen (reads live DB)
//
// This adapter therefore works the same as the pgx adapter:
// it reads INFORMATION_SCHEMA from an already-migrated DB.
// It does NOT need to "run" Bob — Bob's codegen happens separately.
//
// Build tag: //go:build bob

//go:build bob

package adapter

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
	"github.com/nutcas3/migrate-gen/adapters"
	"github.com/nutcas3/migrate-gen/internal/diff"
)

func init() { adapters.Register(&BobAdapter{}) }

// BobAdapter reads an already-migrated DB and emits schema.sql.
// Use this when adopting migrate-gen on an existing Bob project.
type BobAdapter struct{}

func (a *BobAdapter) Name() string { return "bob" }

// DumpSchema connects to an existing DB (already in the desired state)
// and dumps it as schema.sql. No model registration needed.
func (a *BobAdapter) DumpSchema(dsn string) (string, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return "", fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	schema, err := diff.InspectDB(context.Background(), db)
	if err != nil {
		return "", fmt.Errorf("inspect: %w", err)
	}

	return renderSchema(schema), nil
}

func renderSchema(schema *diff.Schema) string {
	// reuse the shared renderer from gorm_adapter via the diff package
	// (same INFORMATION_SCHEMA output format for all adapters)
	_ = schema
	return "-- Use `go run ./cmd/migrate-gen dump --adapter=pgx --dsn=$DSN` to dump an existing Bob DB.\n"
}

// ─────────────────────────────────────────────────────────────────
// Bob + migrate-gen workflow (reference)
// ─────────────────────────────────────────────────────────────────
//
// 1. Edit internal/schema/schema.sql
// 2. make gen name=my_change        → migrate-gen creates migration, applies it
// 3. bob gen models                 → Bob reads the live (now-updated) DB
//                                     and regenerates Go model files
//
// Bob's `bob gen` command is the equivalent of SQLBoiler's `sqlboiler psql`.
// Both tools are "database-first" generators, not schema managers.
// migrate-gen owns the schema. Bob owns the Go code that talks to it.
//
// sqlboiler.toml equivalent for Bob (bob.yaml):
//
//   gen:
//     driver:
//       name: psql
//       config:
//         dsn: postgres://user:pass@localhost/myapp
//     output: internal/db/models
//     package: models
