// cmd/migrate-gen/main.go
//
// migrate-gen — Declarative Migration Engine for any Go framework.
//
// Subcommands:
//   gen   [name]     Generate a new migration from schema.sql diff (default)
//   check            CI mode: exit 1 if schema.sql and migrations/ are out of sync
//   dump             Dump schema.sql from a live DB (for ORM migration projects)
//   lint             Scan migration files for dangerous keywords
//
// Usage:
//   go run ./cmd/migrate-gen gen add_posts_table
//   go run ./cmd/migrate-gen gen --tui add_posts_table  # Interactive TUI mode
//   go run ./cmd/migrate-gen check
//   go run ./cmd/migrate-gen dump --adapter=gorm --dsn=$DEV_DSN
//   go run ./cmd/migrate-gen lint

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	migrate_gen "github.com/nutcas3/migrate-gen"
)

type config struct {
	MigrationsDir string
	SchemaFile    string
	Adapter       string
	DSN           string
}

func defaultConfig() config {
	return config{
		MigrationsDir: env("MIGRATIONS_DIR", "migrations"),
		SchemaFile:    env("SCHEMA_FILE", "internal/schema/schema.sql"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	if len(os.Args) < 2 {
		os.Args = append(os.Args, "gen") // default subcommand
	}

	switch os.Args[1] {
	case "gen":
		runGen(os.Args[2:])
	case "check", "--check-only", "--verify":
		runCheck(os.Args[2:])
	case "dump":
		runDump(os.Args[2:])
	case "lint":
		runLint(os.Args[2:])
	default:
		// Treat bare arguments as migration name: `migrate-gen add_users`
		runGen(os.Args[1:])
	}
}

func runGen(args []string) {
	fs := flag.NewFlagSet("gen", flag.ExitOnError)
	cfg := defaultConfig()
	tui := fs.Bool("tui", false, "Use interactive TUI interface")
	fs.StringVar(&cfg.MigrationsDir, "migrations", cfg.MigrationsDir, "Directory containing migration files")
	fs.StringVar(&cfg.SchemaFile, "schema", cfg.SchemaFile, "Path to schema.sql")
	fs.Parse(args)

	name := strings.Join(fs.Args(), "_")
	if name == "" {
		name = "schema_update"
	}

	if *tui {
		// Run with TUI
		model := InitialModel(cfg, name)
		p := tea.NewProgram(model, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fatalf("Error running TUI: %v", err)
		}
		return
	}

	// Original CLI mode
	ctx := context.Background()

	logf("Starting migrate-gen...")
	logf("Schema file : %s", cfg.SchemaFile)
	logf("Migrations  : %s", cfg.MigrationsDir)

	logf("\n[1/4] Starting shadow DB for current state...")
	currentContainer, err := migrate_gen.Start(ctx)
	must(err, "start current shadow container")
	defer currentContainer.Stop()
	logf("      Container: %s (port %s)", currentContainer.Name, currentContainer.Port)

	logf("[2/4] Applying existing migrations to current shadow DB...")
	must(currentContainer.ApplyMigrations(ctx, cfg.MigrationsDir), "apply existing migrations")

	currentDB, err := currentContainer.DB()
	must(err, "open current DB")
	defer currentDB.Close()

	currentSchema, err := migrate_gen.InspectDB(ctx, currentDB)
	must(err, "inspect current schema")
	logf("      Found %d table(s) in current state.", len(currentSchema.Tables))

	logf("[3/4] Starting shadow DB for desired state...")
	desiredContainer, err := migrate_gen.Start(ctx)
	must(err, "start desired shadow container")
	defer desiredContainer.Stop()
	logf("      Container: %s (port %s)", desiredContainer.Name, desiredContainer.Port)

	logf("      Applying schema.sql...")
	must(desiredContainer.ApplySchemaFile(ctx, cfg.SchemaFile), "apply schema.sql")

	desiredDB, err := desiredContainer.DB()
	must(err, "open desired DB")
	defer desiredDB.Close()

	desiredSchema, err := migrate_gen.InspectDB(ctx, desiredDB)
	must(err, "inspect desired schema")
	logf("      Found %d table(s) in desired state.", len(desiredSchema.Tables))

	// ── Diff ──
	logf("[4/4] Computing diff...")
	result := migrate_gen.Diff(currentSchema, desiredSchema)

	if result.IsEmpty() {
		logf("\n✅ No schema drift detected. Migrations are in sync with schema.sql.")
		os.Exit(0)
	}

	// Print warnings before writing
	if len(result.Warnings) > 0 {
		logf("\n⚠️  WARNINGS (require manual review before applying):")
		for _, w := range result.Warnings {
			logf("   • %s", w)
		}
	}

	// ── Write files ──
	written, err := migrate_gen.WriteMigration(result, migrate_gen.WriteOptions{
		MigrationsDir: cfg.MigrationsDir,
		Name:          name,
	})
	must(err, "write migration files")

	logf("\n✅ Migration generated:")
	for _, f := range written {
		logf("   %s", f)
	}

	if result.HasDestructive {
		logf("\n⚠️  Destructive statements are COMMENTED OUT in the migration.")
		logf("   Review, uncomment, and re-run `make migrate-up` when ready.")
	}
}

func runCheck(args []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	cfg := defaultConfig()
	fs.StringVar(&cfg.MigrationsDir, "migrations", cfg.MigrationsDir, "Migrations directory")
	fs.StringVar(&cfg.SchemaFile, "schema", cfg.SchemaFile, "Path to schema.sql")
	fs.Parse(args)

	ctx := context.Background()

	currentContainer, err := migrate_gen.Start(ctx)
	must(err, "start current container")
	defer currentContainer.Stop()
	must(currentContainer.ApplyMigrations(ctx, cfg.MigrationsDir), "apply migrations")

	desiredContainer, err := migrate_gen.Start(ctx)
	must(err, "start desired container")
	defer desiredContainer.Stop()
	must(desiredContainer.ApplySchemaFile(ctx, cfg.SchemaFile), "apply schema.sql")

	currentDB, err := currentContainer.DB()
	must(err, "open current DB")
	defer currentDB.Close()
	desiredDB, err := desiredContainer.DB()
	must(err, "open desired DB")
	defer desiredDB.Close()

	currentSchema, err := migrate_gen.InspectDB(ctx, currentDB)
	must(err, "inspect current")
	desiredSchema, err := migrate_gen.InspectDB(ctx, desiredDB)
	must(err, "inspect desired")

	result := migrate_gen.Diff(currentSchema, desiredSchema)
	check := migrate_gen.FormatCheckOutput(result)

	if check.InSync {
		fmt.Println("✅ Schema is in sync. No migration needed.")
		os.Exit(0)
	}

	fmt.Fprintln(os.Stderr, "❌ Schema drift detected!")
	fmt.Fprintln(os.Stderr, "   schema.sql contains changes not yet in migrations/.")
	fmt.Fprintln(os.Stderr, "   Run: make gen name=<migration_name>")
	fmt.Fprintln(os.Stderr, "\nPending changes:")
	for _, c := range check.Changes {
		fmt.Fprintln(os.Stderr, c)
	}
	os.Exit(1)
}

func runDump(args []string) {
	fs := flag.NewFlagSet("dump", flag.ExitOnError)
	adapter := fs.String("adapter", "pgx", "Framework adapter: gorm | bun | beego | bob | pgx")
	dsn := fs.String("dsn", os.Getenv("DATABASE_URL"), "DSN of the source database")
	fs.Parse(args)

	if *dsn == "" {
		fatalf("--dsn is required for dump (or set DATABASE_URL)")
	}

	// pgx / raw SQL dump (no ORM dependency)
	if *adapter == "pgx" {
		ctx := context.Background()
		db, err := openSQL(*dsn)
		must(err, "open DB")
		defer db.Close()

		schema, err := migrate_gen.InspectDB(ctx, db)
		must(err, "inspect DB")
		fmt.Print(renderSchemaSQL(schema))
		return
	}

	// ORM adapters — compiled only with their respective build tags
	fatalf("Adapter %q requires building with: go run -tags %s ./cmd/migrate-gen dump", *adapter, *adapter)
}

func runLint(args []string) {
	fs := flag.NewFlagSet("lint", flag.ExitOnError)
	cfg := defaultConfig()
	fs.StringVar(&cfg.MigrationsDir, "migrations", cfg.MigrationsDir, "Migrations directory")
	fs.Parse(args)

	dangerous := []string{
		"DROP TABLE", "TRUNCATE", "DROP DATABASE",
		"DROP SCHEMA", "DELETE FROM", "UPDATE ", // UPDATE without WHERE
	}

	entries, err := os.ReadDir(cfg.MigrationsDir)
	must(err, "read migrations dir")

	found := false
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		content, err := os.ReadFile(cfg.MigrationsDir + "/" + e.Name())
		must(err, "read file")

		upper := strings.ToUpper(string(content))
		for _, kw := range dangerous {
			// Skip if it's inside a SQL comment line
			for line := range strings.SplitSeq(upper, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "--") {
					continue // commented out — safe
				}
				if strings.Contains(trimmed, kw) {
					fmt.Fprintf(os.Stderr, "⚠️  [%s] contains %q — requires senior engineer review\n",
						e.Name(), kw)
					found = true
				}
			}
		}
	}

	if found {
		fmt.Fprintln(os.Stderr, "\nLint failed. Review flagged statements before merging.")
		os.Exit(1)
	}
	fmt.Println("✅ Lint passed. No dangerous keywords found in active statements.")
}

func must(err error, context string) {
	if err != nil {
		fatalf("%s: %v", context, err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "migrate-gen: "+format+"\n", args...)
	os.Exit(1)
}

func logf(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
}

func openSQL(_ string) (*sql.DB, error) {
	// Import cycle guard — the real open is in shadow package.
	// For the dump command we use lib/pq directly.
	return nil, fmt.Errorf("import openSQL from shadow package")
}

// renderSchemaSQL converts an inspected Schema into a schema.sql string.
// Shared by dump command and all adapters.
func renderSchemaSQL(schema *migrate_gen.Schema) string {
	var sb strings.Builder
	sb.WriteString("-- schema.sql — generated by `migrate-gen dump`\n")
	sb.WriteString("-- Edit this file. Use `make gen` to generate migrations from changes.\n\n")

	for tname, t := range schema.Tables {
		var lines []string
		for _, colName := range t.ColOrder {
			col := t.Columns[colName]
			line := fmt.Sprintf("    %q %s", colName, col.FullType)
			if !col.IsNullable {
				line += " NOT NULL"
			}
			if col.DefaultValue.Valid {
				line += " DEFAULT " + col.DefaultValue.String
			}
			lines = append(lines, line)
		}
		if len(t.PrimaryKeys) > 0 {
			pks := make([]string, len(t.PrimaryKeys))
			for i, pk := range t.PrimaryKeys {
				pks[i] = fmt.Sprintf("%q", pk)
			}
			lines = append(lines, "    PRIMARY KEY ("+strings.Join(pks, ", ")+")")
		}
		for _, fk := range t.ForeignKeys {
			onDel := ""
			if fk.OnDelete != "" && fk.OnDelete != "NO ACTION" {
				onDel = " ON DELETE " + fk.OnDelete
			}
			lines = append(lines, fmt.Sprintf(
				"    CONSTRAINT %q FOREIGN KEY (%q) REFERENCES %q (%q)%s",
				fk.ConstraintName, fk.Column, fk.RefTable, fk.RefColumn, onDel,
			))
		}
		sb.WriteString(fmt.Sprintf("CREATE TABLE %q (\n%s\n);\n\n",
			tname, strings.Join(lines, ",\n")))
	}

	for _, idx := range schema.Indexes {
		u := ""
		if idx.IsUnique {
			u = "UNIQUE "
		}
		cols := make([]string, len(idx.Columns))
		for i, c := range idx.Columns {
			cols[i] = fmt.Sprintf("%q", c)
		}
		sb.WriteString(fmt.Sprintf("CREATE %sINDEX %q ON %q USING %s (%s);\n",
			u, idx.Name, idx.TableName, idx.Method, strings.Join(cols, ", ")))
	}

	return sb.String()
}
