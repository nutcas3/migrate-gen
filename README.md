# migrate-gen — Declarative Migration Engine for Go

A framework-agnostic, Shadow-DB-powered migration generator with **modern TUI**.
**No DSL. No ORM magic. Just SQL you can read.**

---

## Installation

```bash
go install github.com/nutcas3/migrate-gen/cmd/migrate-gen@latest
```

Or use directly in your project:

```bash
# Add to your project
go get github.com/nutcas3/migrate-gen

# Run commands
go run ./cmd/migrate-gen gen add_posts_table
go run ./cmd/migrate-gen gen --tui add_posts_table  # Interactive TUI mode
```

### Dependencies

- **Docker** (required for shadow containers)
- **Go 1.26+** (for building)

---

## 🎨 Interactive TUI (NEW!)

migrate-gen now features a beautiful, interactive terminal UI powered by Bubbletea and Charm!

### TUI Mode

```bash
go run ./cmd/migrate-gen gen --tui add_posts_table
```

**Features:**
- 🎯 **Real-time Progress**: Visual progress bar through all migration phases
- ⏳ **Active Indicators**: Animated spinners for each operation
- 🎨 **Beautiful Styling**: Modern colors and bordered layouts
- ⚠️ **Warning Display**: Highlighted warnings in styled boxes
- ✅ **Clear Feedback**: Success/error states with detailed information
- 🔧 **Error Handling**: Graceful error display with user guidance

**TUI Workflow:**
```
🚀 migrate-gen
────────────────────────────────────────────────────────────────────────────────────────
⏳ Starting shadow DB for current state...
Progress: ████████████████████████████████████████████████████████████ 100%

⚠️  Warnings:
• TABLE REMOVED: "old_table" exists in DB but not in schema.sql
• COLUMN REMOVED: "unused_column" is in the DB but not in schema.sql

✅ Migration generated successfully!
Files created:
• migrations/000004_add_posts_table.up.sql
• migrations/000004_add_posts_table.down.sql
────────────────────────────────────────────────────────────────────────────────────────
```

### Traditional CLI Mode

```bash
go run ./cmd/migrate-gen gen add_posts_table
```

Both modes provide the same powerful functionality - choose your preferred interface!

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                   internal/schema/schema.sql                │
│                    THE ONLY FILE YOU EDIT                   │
└──────────────────────────┬──────────────────────────────────┘
                           │
           ┌───────────────┼────────────────────┐
           │               │                    │
           ▼               ▼                    ▼
    migrate-gen         sqlc gen          sqlboiler / bob
    (diff engine)    (query funcs)        (ORM models)
           │
    ┌──────┴───────┐
    │              │
    ▼              ▼
Shadow DB 1    Shadow DB 2
(migrations    (schema.sql
 applied)       applied)
    │              │
    └──────┬───────┘
           │ INFORMATION_SCHEMA diff
           ▼
  migrations/000X_name.up.sql
  migrations/000X_name.down.sql
           │
           ▼
  golang-migrate applies to prod
```

### The Two Shadow Containers

Every `make gen` run spins up **two throwaway Docker Postgres containers**:

| Container | Purpose | How it's populated |
|-----------|---------|-------------------|
| `current` | What your DB looks like right now | All `migrations/*.up.sql` applied in order |
| `desired` | What you want it to look like | `schema.sql` applied directly |

The engine then runs `INFORMATION_SCHEMA` queries against both and computes the diff. The diff becomes your new versioned migration file.

---

## Framework Support Matrix

| Framework | Role | How it integrates |
|-----------|------|-------------------|
| **sqlc** | Query → Go code generator | Reads same `schema.sql`. `make gen` calls `sqlc generate`. |
| **SQLBoiler** | DB-first ORM generator | Runs after migrate-up. Generates `internal/db/models/`. |
| **Bob** | Modern SQL generator | Runs after migrate-up. Same pattern as SQLBoiler. |
| **pgx/v5** | Postgres driver | Used by all of the above as the underlying driver. |
| **GORM** | ORM (escape path) | `make dump-gorm` → one-time export to `schema.sql`. |
| **Bun** | ORM (escape path) | `make dump-bun` → one-time export to `schema.sql`. |
| **Beego ORM** | ORM (escape path) | `make dump-beego` → one-time export to `schema.sql`. |

---

## 📋 CLI Commands

| Command | Description | TUI Support |
|---------|-------------|-------------|
| `gen [name]` | Generate migration from schema.sql diff | ✅ `--tui` flag |
| `check` | CI mode: exit 1 if schema.sql ≠ migrations/ | ❌ (text-only) |
| `dump` | Export schema from live DB to schema.sql | ❌ (text-only) |
| `lint` | Scan migrations for dangerous keywords | ❌ (text-only) |

### Usage Examples

```bash
# Interactive TUI (recommended)
go run ./cmd/migrate-gen gen --tui add_users_table

# Traditional CLI
go run ./cmd/migrate-gen gen add_users_table

# CI/CD verification
go run ./cmd/migrate-gen check

# Export from existing database
go run ./cmd/migrate-gen dump --adapter=gorm --dsn=$DATABASE_URL

# Safety check
go run ./cmd/migrate-gen lint
```

### Flags

```bash
--tui              Enable interactive TUI (gen command only)
--migrations DIR   Migrations directory (default: migrations/)
--schema FILE      Schema file path (default: internal/schema/schema.sql)
--adapter NAME     ORM adapter for dump (gorm|bun|beego|bob|pgx)
--dsn URL          Database connection string for dump
```

---

## Daily Developer Workflow

### Step 1: Edit schema.sql

```sql
-- Add a table to internal/schema/schema.sql:
CREATE TABLE "comments" (
    "id"         bigserial    NOT NULL,
    "post_id"    bigint       NOT NULL,
    "user_id"    bigint       NOT NULL,
    "body"       text         NOT NULL,
    "created_at" timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY ("id"),
    CONSTRAINT "fk_comments_post"
        FOREIGN KEY ("post_id") REFERENCES "posts" ("id") ON DELETE CASCADE,
    CONSTRAINT "fk_comments_user"
        FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE
);
CREATE INDEX "idx_comments_post_id" ON "comments" USING btree ("post_id");
```

### Step 2: Generate

#### Interactive TUI Mode (Recommended)

```bash
go run ./cmd/migrate-gen gen --tui add_comments_table
```

#### Traditional CLI Mode

```bash
go run ./cmd/migrate-gen gen add_comments_table
```

**TUI Output:**
```
🚀 migrate-gen
────────────────────────────────────────────────────────────────────────────────────────
⏳ Starting shadow DB for current state...
Progress: ████████████████████████████████████████████████████████████ 25%

⏳ Applying existing migrations...
Progress: ████████████████████████████████████████████████████████████ 50%

⏳ Computing schema diff...
Progress: ████████████████████████████████████████████████████████████ 100%

✅ Migration generated successfully!
Files created:
• migrations/000003_add_comments_table.up.sql
• migrations/000003_add_comments_table.down.sql
────────────────────────────────────────────────────────────────────────────────────────
```

**CLI Output:**
```
━━━ migrate-gen ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
[1/4] Starting shadow DB for current state...
      Container: migrate-gen-shadow-12345 (port 49201)
[2/4] Applying existing migrations to current shadow DB...
      Found 2 table(s) in current state.
[3/4] Starting shadow DB for desired state...
      Found 5 table(s) in desired state.
[4/4] Computing diff...

✅ Migration generated:
   migrations/000003_add_comments_table.up.sql
   migrations/000003_add_comments_table.down.sql
```

### Step 3: Apply

```bash
make migrate-up
```

### Step 4: Write queries (sqlc path)

Add to `internal/schema/queries/comments.sql`:

```sql
-- name: ListCommentsByPost :many
SELECT id, user_id, body, created_at
FROM comments
WHERE post_id = $1
ORDER BY created_at ASC;
```

Run `make gen-queries`. You get:

```go
func (q *Queries) ListCommentsByPost(ctx context.Context, postID int64) ([]Comment, error)
```

---

## Safety Model

The engine never auto-emits destructive SQL. Instead:

| Change | Behaviour |
|--------|-----------|
| New table | `CREATE TABLE` emitted in `.up.sql` |
| New column | `ADD COLUMN` emitted |
| Type change | `ALTER COLUMN TYPE ... USING` emitted, **flagged** |
| NOT NULL added | `SET NOT NULL` emitted, **flagged** |
| Column removed | Commented-out `DROP COLUMN` + warning |
| Table removed | Commented-out `DROP TABLE` + warning |

Flagged statements require a human to uncomment them. The CI `lint` step flags uncommented `DROP TABLE` / `TRUNCATE` for senior-engineer review.

---

## CI/CD Pipeline

```yaml
# .github/workflows/schema.yml
on:
  pull_request:
    paths:
      - 'internal/schema/**'
      - 'migrations/**'

jobs:
  schema-check:
    runs-on: ubuntu-latest
    services:
      postgres:                    # not used by check — shadow containers spin their own
        image: postgres:16-alpine
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: make check            # exits 1 if schema.sql ≠ migrations/
      - run: make lint             # exits 1 if dangerous keywords in active SQL

  deploy:
    needs: schema-check
    runs-on: ubuntu-latest
    steps:
      - run: migrate -path migrations -database ${{ secrets.PROD_DSN }} up
      # Production NEVER sees migrate-gen, schema.sql, or sqlc/sqlboiler.
      # It only sees the versioned SQL files in migrations/.
```

---

## Migrating from an ORM (Escape Path)

### From GORM AutoMigrate

```bash
# 1. Capture current schema (one-time, run against your dev DB)
make dump-gorm

# 2. Review internal/schema/schema.sql
# 3. Commit it
# 4. Remove all AutoMigrate() calls from your application
# 5. From now on: make gen → make migrate-up
```

### From Bun / Beego

Same pattern — use `make dump-bun` or `make dump-beego` respectively.

---

## Project Structure

```
migrate-gen/
├── cmd/migrate-gen/
│   ├── main.go                     # CLI: gen | check | dump | lint
│   └── tui.go                      # 🎨 Interactive TUI (Bubbletea + Charm)
├── models/                         # 📁 Shared data models
│   ├── schema.go                   # Schema, Table, Column, Index, ForeignKey
│   ├── diff.go                     # Result, Statement, WriteOptions, CheckResult
│   └── shadow.go                   # Container model
├── internal/                       # Private implementation
│   ├── diff/
│   │   ├── inspector.go            # INFORMATION_SCHEMA queries
│   │   ├── engine.go               # current → desired diff logic
│   │   └── writer.go               # .up.sql / .down.sql file writer
│   ├── shadow/
│   │   └── shadow.go               # Docker container lifecycle
│   ├── schema/
│   │   ├── schema.sql              # ← EDIT THIS. Nothing else.
│   │   └── queries/                # sqlc query files
│   │       └── users.sql
│   └── adapters/
│       ├── adapters.go             # Adapter interface + registry
│       ├── gorm/gorm_adapter.go    # build tag: gorm
│       ├── bun/bun_adapter.go      # build tag: bun
│       ├── beego/beego_adapter.go  # build tag: beego
│       └── bob/bob_adapter.go      # build tag: bob
├── migrations/                     # Generated SQL — commit these
│   ├── 000001_init.up.sql
│   └── 000001_init.down.sql
├── internal/db/
│   ├── sqlc/                       # Generated by sqlc — DO NOT EDIT
│   └── models/                     # Generated by SQLBoiler/Bob — DO NOT EDIT
├── sqlc.yaml
├── sqlboiler.toml                  # (optional)
├── bob.yaml                        # (optional)
├── go.mod
└── Makefile
```
