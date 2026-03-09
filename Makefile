# Makefile — migrate-gen Developer Control Surface
#
# Prerequisites:
#   docker          (for shadow DB containers)
#   golang-migrate  (go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest)
#   sqlc            (go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest)
#   sqlboiler       (go install github.com/volatiletech/sqlboiler/v4@latest)         [optional]
#   bob             (go install github.com/stephenafamo/bob/gen/bobgen-psql@latest)   [optional]

DB_URL  ?= postgres://user:pass@localhost:5432/myapp?sslmode=disable
SCHEMA  ?= internal/schema/schema.sql
MIGDIR  ?= migrations

## gen [name=<migration_name>]
## Edit schema.sql, then run this. Generates the migration + all Go code.
##
## Example:
##   make gen name=add_posts_table
##   make gen name=add_audit_log
gen:
	@echo "━━━ migrate-gen ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@go run ./cmd/migrate-gen gen $(name) \
		--schema  $(SCHEMA) \
		--migrations $(MIGDIR)
	@echo ""
	@echo "━━━ sqlc generate ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@sqlc generate
	@echo ""
	@if [ -f sqlboiler.toml ]; then \
		echo "━━━ sqlboiler ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"; \
		sqlboiler psql --config sqlboiler.toml --wipe; \
		echo ""; \
	fi
	@if [ -f bob.yaml ]; then \
		echo "━━━ bob gen ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"; \
		bobgen-psql --config bob.yaml; \
		echo ""; \
	fi
	@echo "✅ Done. Commit: migrations/, internal/db/sqlc/, internal/db/models/"

## migrate-up
## Apply all pending migrations to your local dev DB.
migrate-up:
	@migrate -path $(MIGDIR) -database "$(DB_URL)" up

## migrate-down [n=1]
## Roll back N migrations (default: 1).
migrate-down:
	@migrate -path $(MIGDIR) -database "$(DB_URL)" down $(or $(n),1)

## migrate-status
## Show which migrations have been applied.
migrate-status:
	@migrate -path $(MIGDIR) -database "$(DB_URL)" version

## check
## Fails if schema.sql has changes not yet turned into a migration.
## Run this in CI on every PR that touches internal/schema/.
check:
	@go run ./cmd/migrate-gen check \
		--schema     $(SCHEMA) \
		--migrations $(MIGDIR)

## lint
## Scan migration files for dangerous keywords (DROP TABLE, TRUNCATE, etc.)
## Flags files that need senior-engineer sign-off.
lint:
	@go run ./cmd/migrate-gen lint --migrations $(MIGDIR)

## ci
## Full CI gate: drift check + lint. Both must pass.
ci: check lint

## dump-gorm
## One-time: capture your GORM AutoMigrate schema as schema.sql.
## After this, stop using AutoMigrate and use `make gen` instead.
##
## Requires: go run -tags gorm ./cmd/migrate-gen dump
dump-gorm:
	@go run -tags gorm ./cmd/migrate-gen dump \
		--adapter gorm \
		--dsn     "$(DB_URL)" \
		> $(SCHEMA)
	@echo "✅ schema.sql written from GORM structs. Review it, then commit."

## dump-bun
## One-time: capture your Bun schema as schema.sql.
dump-bun:
	@go run -tags bun ./cmd/migrate-gen dump \
		--adapter bun \
		--dsn     "$(DB_URL)" \
		> $(SCHEMA)
	@echo "✅ schema.sql written from Bun models."

## dump-beego
## One-time: capture your Beego ORM schema as schema.sql.
dump-beego:
	@go run -tags beego ./cmd/migrate-gen dump \
		--adapter beego \
		--dsn     "$(DB_URL)" \
		> $(SCHEMA)
	@echo "✅ schema.sql written from Beego ORM models."

## dump-pgx
## Dump schema.sql from any existing Postgres DB (no ORM required).
dump-pgx:
	@go run ./cmd/migrate-gen dump \
		--adapter pgx \
		--dsn     "$(DB_URL)" \
		> $(SCHEMA)
	@echo "✅ schema.sql written from live database."

## dev-db
## Spin up a local Postgres container for development.
dev-db:
	@docker run --rm -d \
		--name myapp-dev \
		-e POSTGRES_PASSWORD=pass \
		-e POSTGRES_USER=user \
		-e POSTGRES_DB=myapp \
		-p 5432:5432 \
		postgres:16-alpine
	@echo "✅ Dev DB running → postgres://user:pass@localhost:5432/myapp"

dev-db-stop:
	@docker stop myapp-dev 2>/dev/null || true

## reset
## Drop and recreate the local DB, then apply all migrations from scratch.
## USE IN DEV ONLY.
reset: dev-db-stop dev-db
	@sleep 2
	@migrate -path $(MIGDIR) -database "$(DB_URL)" drop -f
	@migrate -path $(MIGDIR) -database "$(DB_URL)" up
	@echo "✅ Local DB reset and all migrations applied."

help:
	@echo ""
	@echo "migrate-gen — Declarative Migration Engine"
	@echo ""
	@echo "  make gen name=<name>    Generate migration + sqlc + SQLBoiler/Bob"
	@echo "  make migrate-up         Apply pending migrations"
	@echo "  make migrate-down       Roll back one migration"
	@echo "  make check              CI: detect schema drift"
	@echo "  make lint               CI: flag dangerous SQL"
	@echo "  make ci                 CI: check + lint"
	@echo "  make dump-gorm          One-time GORM → schema.sql"
	@echo "  make dump-bun           One-time Bun  → schema.sql"
	@echo "  make dump-beego         One-time Beego → schema.sql"
	@echo "  make dump-pgx           Dump any live DB → schema.sql"
	@echo "  make dev-db             Start local Postgres (Docker)"
	@echo "  make reset              Wipe and recreate local DB"
	@echo ""

.PHONY: gen migrate-up migrate-down migrate-status check lint ci \
        dump-gorm dump-bun dump-beego dump-pgx dev-db dev-db-stop reset help
