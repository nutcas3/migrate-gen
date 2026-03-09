// internal/adapters/bun/bun_adapter.go
//
// Bun → schema.sql adapter.
//
// Bun is "programmatic" — you call db.NewCreateTable().Model(&User{}).Exec(ctx).
// This adapter does exactly that against a throwaway DB, then reads the result.
//
// Build tag: //go:build bun

//go:build bun

package adapter

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/nutcas3/migrate-gen/pkg/adapters"
	gorm_adapter "github.com/nutcas3/migrate-gen/pkg/adapters/gorm" // reuse dumpToSQL
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
)

func init() { adapters.Register(&BunAdapter{}) }

// BunAdapter implements adapters.Adapter for Bun.
type BunAdapter struct {
	// Models is populated by the caller.
	// e.g. adapter.Models = []interface{}{(*User)(nil), (*Post)(nil)}
	Models []interface{}
}

func (a *BunAdapter) Name() string { return "bun" }

// DumpSchema creates all tables via Bun's CreateTable, then reads the schema.
// dsn must point to a THROWAWAY database.
func (a *BunAdapter) DumpSchema(dsn string) (string, error) {
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	db := bun.NewDB(sqldb, bun.IDialect(nil)) // dialect set via pgdriver

	ctx := context.Background()
	for _, model := range a.Models {
		if _, err := db.NewCreateTable().Model(model).IfNotExists().Exec(ctx); err != nil {
			return "", fmt.Errorf("bun CreateTable %T: %w", model, err)
		}
	}

	return gorm_adapter.DumpToSQL(sqldb) // shared INFORMATION_SCHEMA reader
}

// ─────────────────────────────────────────────────────────────────
// Bun model tag → SQL mapping (reference)
// ─────────────────────────────────────────────────────────────────
//
//   type User struct {
//       bun.BaseModel `bun:"table:users"`
//       ID        int64  `bun:",pk,autoincrement"`
//       Name      string `bun:",notnull"`
//       Email     string `bun:",unique,notnull"`
//   }
//
// Bun emits:
//   CREATE TABLE "users" (
//       "id"    bigserial PRIMARY KEY,
//       "name"  text NOT NULL,
//       "email" text NOT NULL,
//       UNIQUE ("email")
//   );
