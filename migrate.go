// Package migrate_gen provides a declarative migration engine for Go.
//
// The primary workflow is:
//  1. Create two shadow databases (current state from migrations, desired state from schema.sql)
//  2. Inspect both databases to get their schemas
//  3. Compute the diff between current and desired
//  4. Generate migration files from the diff
//
// Safety rules:
//   - DROP TABLE operations are commented out and require manual review
//   - DROP COLUMN operations are commented out and require manual review
//   - TYPE changes are flagged for review
//   - All destructive operations require human intervention
package adapters

import (
	"context"
	"database/sql"

	"github.com/nutcas3/migrate-gen/internal/diff"
	"github.com/nutcas3/migrate-gen/internal/shadow"
	"github.com/nutcas3/migrate-gen/models"
)

// Schema represents a database schema snapshot.
type Schema = models.Schema

// Result holds the diff result between two schemas.
type Result = models.Result

// Statement represents a SQL statement with metadata.
type Statement = models.Statement

// WriteOptions controls how migration files are written.
type WriteOptions = models.WriteOptions

// CheckResult represents the result of a schema sync check.
type CheckResult = models.CheckResult

// Container represents a running shadow database container.
type Container struct {
	inner *shadow.Container
	Name  string
	Port  string
	DSN   string
}

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
	c, err := shadow.Start(ctx)
	if err != nil {
		return nil, err
	}
	return &Container{
		inner: c,
		Name:  c.Name,
		Port:  c.Port,
		DSN:   c.DSN,
	}, nil
}

// Stop tears down the container.
func (c *Container) Stop() {
	c.inner.Stop()
}

// DB opens and returns a *sql.DB for this container.
func (c *Container) DB() (*sql.DB, error) {
	return c.inner.DB()
}

// ApplyMigrations applies migration files to the container.
func (c *Container) ApplyMigrations(ctx context.Context, migrationsDir string) error {
	return c.inner.ApplyMigrations(ctx, migrationsDir)
}

// ApplySchemaFile applies a schema file to the container.
func (c *Container) ApplySchemaFile(ctx context.Context, schemaPath string) error {
	return c.inner.ApplySchemaFile(ctx, schemaPath)
}
