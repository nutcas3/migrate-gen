// internal/shadow/shadow.go
//
// Manages ephemeral Postgres Docker containers used as shadow databases.
// The engine spins up TWO per run:
//   - "current"  → all existing migrations applied  (what DB looks like now)
//   - "desired"  → schema.sql applied directly      (what we want it to look like)
//
// Both are torn down on exit. They never touch your real database.

package shadow

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

const (
	shadowImage    = "postgres:16-alpine"
	shadowUser     = "shadow"
	shadowPassword = "shadow"
	shadowDB       = "shadow"
	readyTimeout   = 30 * time.Second
)

// Container represents a running shadow Postgres instance.
type Container struct {
	Name string
	Port string
	DSN  string
}

// Start spins up a new ephemeral Postgres container and returns it when ready.
func Start(ctx context.Context) (*Container, error) {
	name := fmt.Sprintf("migrate-gen-shadow-%d-%d", os.Getpid(), rand.Intn(99999))

	cmd := exec.CommandContext(ctx, "docker", "run",
		"--rm",                  // auto-remove when stopped
		"-d",                    // detached
		"--name", name,
		"-e", "POSTGRES_USER="+shadowUser,
		"-e", "POSTGRES_PASSWORD="+shadowPassword,
		"-e", "POSTGRES_DB="+shadowDB,
		"-P",                    // random available host port
		"--health-cmd", "pg_isready -U shadow",
		"--health-interval", "1s",
		"--health-retries", "20",
		shadowImage,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("docker run: %w\n%s", err, out)
	}

	port, err := waitForPort(ctx, name)
	if err != nil {
		stopContainer(name) // best-effort cleanup
		return nil, fmt.Errorf("container not ready: %w", err)
	}

	dsn := fmt.Sprintf(
		"postgres://%s:%s@localhost:%s/%s?sslmode=disable",
		shadowUser, shadowPassword, port, shadowDB,
	)

	// Wait for Postgres itself to accept connections
	if err := waitForPostgres(ctx, dsn); err != nil {
		stopContainer(name)
		return nil, fmt.Errorf("postgres not ready: %w", err)
	}

	return &Container{Name: name, Port: port, DSN: dsn}, nil
}

// Stop tears down the container. Safe to call multiple times.
func (c *Container) Stop() {
	stopContainer(c.Name)
}

// DB opens and returns a *sql.DB for this container.
func (c *Container) DB() (*sql.DB, error) {
	db, err := sql.Open("postgres", c.DSN)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(5)
	return db, nil
}

// ApplyMigrations applies every *.up.sql file in migrationsDir, in order.
// This brings the shadow DB to the "current" state.
func (c *Container) ApplyMigrations(ctx context.Context, migrationsDir string) error {
	files, err := upMigrationFiles(migrationsDir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil // first run — no migrations yet
	}

	db, err := c.DB()
	if err != nil {
		return err
	}
	defer db.Close()

	for _, f := range files {
		sqlBytes, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}
		if _, err := db.ExecContext(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("apply %s: %w", f, err)
		}
	}
	return nil
}

// ApplySchemaFile executes a raw SQL file (schema.sql) against the container.
// This brings the shadow DB to the "desired" state.
func (c *Container) ApplySchemaFile(ctx context.Context, schemaPath string) error {
	sqlBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}

	db, err := c.DB()
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("apply schema.sql: %w\n\nCheck your schema.sql for syntax errors.", err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────

func waitForPort(ctx context.Context, name string) (string, error) {
	deadline := time.Now().Add(readyTimeout)
	for time.Now().Before(deadline) {
		out, err := exec.CommandContext(ctx, "docker", "port", name, "5432/tcp").Output()
		if err == nil {
			// Output: "0.0.0.0:54321\n" or ":::54321\n"
			line := strings.TrimSpace(string(out))
			parts := strings.Split(line, ":")
			if len(parts) > 0 {
				return parts[len(parts)-1], nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return "", fmt.Errorf("timeout waiting for docker port after %s", readyTimeout)
}

func waitForPostgres(ctx context.Context, dsn string) error {
	deadline := time.Now().Add(readyTimeout)
	for time.Now().Before(deadline) {
		db, err := sql.Open("postgres", dsn)
		if err == nil {
			if pingErr := db.PingContext(ctx); pingErr == nil {
				db.Close()
				return nil
			}
			db.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("postgres not ready after %s", readyTimeout)
}

func stopContainer(name string) {
	exec.Command("docker", "stop", name).Run() //nolint:errcheck
}

// upMigrationFiles returns all *.up.sql files in dir, sorted numerically.
func upMigrationFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}

	// Sort by the leading numeric prefix (000001, 000002, …)
	sort.Slice(files, func(i, j int) bool {
		return seqNum(files[i]) < seqNum(files[j])
	})
	return files, nil
}

func seqNum(path string) int {
	base := filepath.Base(path)
	parts := strings.SplitN(base, "_", 2)
	if len(parts) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(parts[0])
	return n
}
