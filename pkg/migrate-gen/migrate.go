// Package migrate-gen provides a declarative migration engine for Go.
//
// The primary workflow is:
//   1. Create two shadow databases (current state from migrations, desired state from schema.sql)
//   2. Inspect both databases to get their schemas
//   3. Compute the diff between current and desired
//   4. Generate migration files from the diff
//
// Safety rules:
//   - DROP TABLE operations are commented out and require manual review
//   - DROP COLUMN operations are commented out and require manual review
//   - TYPE changes are flagged for review
//   - All destructive operations require human intervention
package migrate_gen

import (
	"context"
	"database/sql"

	"github.com/nutcas3/migrate-gen/internal/diff"
	"github.com/nutcas3/migrate-gen/internal/shadow"
)

// Schema represents a database schema snapshot.
type Schema = diff.Schema

// Result holds the diff result between two schemas.
type Result = diff.Result

// Statement represents a SQL statement with metadata.
type Statement = diff.Statement

// WriteOptions controls how migration files are written.
type WriteOptions = diff.WriteOptions

// CheckResult represents the result of a schema sync check.
type CheckResult = diff.CheckResult

// Container represents a running shadow database container.
type Container = shadow.Container

// InspectDB connects to a database and returns its schema.
func InspectDB(ctx context.Context, db *sql.DB) (*Schema, error) {
	return diff.InspectDB(ctx, db)
}

// Diff computes what SQL is needed to bring current schema to match desired schema.
func Diff(current, desired *Schema) *Result {
	return diff.Diff(current, desired)
}

// WriteMigration writes .up.sql and .down.sql files from a diff result.
func WriteMigration(result *Result, opts WriteOptions) ([]string, error) {
	return diff.WriteMigration(result, opts)
}

// FormatCheckOutput formats a diff result for CI/check output.
func FormatCheckOutput(result *Result) *CheckResult {
	return diff.FormatCheckOutput(result)
}

// Start creates a new shadow database container.
func Start(ctx context.Context) (*Container, error) {
	return shadow.Start(ctx)
}
