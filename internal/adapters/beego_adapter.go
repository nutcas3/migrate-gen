// internal/adapters/beego/beego_adapter.go
//
// Beego ORM → schema.sql adapter.
//
// Beego's ORM can sync models to a DB (it calls CREATE TABLE / ALTER TABLE
// on startup). This adapter triggers that sync against a throwaway DB and
// then reads the result via INFORMATION_SCHEMA.
//
// Build tag: //go:build beego

//go:build beego

package adapter

import (
	"database/sql"
	"fmt"
	"os"

	beegorm "github.com/beego/beego/v2/client/orm"
	_ "github.com/lib/pq"
	"github.com/nutcas3/migrate-gen/pkg/adapters"
	gorm_adapter "github.com/nutcas3/migrate-gen/pkg/adapters/gorm"
)

func init() { adapters.Register(&BeegoAdapter{}) }

// BeegoAdapter implements adapters.Adapter for Beego ORM.
type BeegoAdapter struct {
	// Models is populated by the caller BEFORE DumpSchema is called.
	// e.g. adapter.Models = []interface{}{new(User), new(Post)}
	Models []interface{}
}

func (a *BeegoAdapter) Name() string { return "beego" }

// DumpSchema registers models with Beego, syncs to a throwaway DB,
// then reads the schema via INFORMATION_SCHEMA.
func (a *BeegoAdapter) DumpSchema(dsn string) (string, error) {
	// Beego reads the alias "default" from its internal registry
	if err := beegorm.RegisterDataBase("default", "postgres", dsn); err != nil {
		return "", fmt.Errorf("beego RegisterDataBase: %w", err)
	}

	for _, m := range a.Models {
		beegorm.RegisterModel(m)
	}

	// RunSyncdb creates/alters tables to match registered models
	// verbose=false, force=false (safe mode — no DROP TABLE)
	if err := beegorm.RunSyncdb("default", false, false); err != nil {
		return "", fmt.Errorf("beego RunSyncdb: %w", err)
	}

	sqlDB, err := sql.Open("postgres", dsn)
	if err != nil {
		return "", err
	}
	defer sqlDB.Close()

	return gorm_adapter.DumpToSQL(sqlDB)
}

// ─────────────────────────────────────────────────────────────────
// Beego model → SQL mapping (reference)
// ─────────────────────────────────────────────────────────────────
//
//   type User struct {
//       Id      int    `orm:"auto"`
//       Name    string `orm:"size(100)"`
//       Email   string `orm:"unique"`
//   }
//
// Beego emits:
//   CREATE TABLE "user" (
//       "id"    serial    NOT NULL PRIMARY KEY,
//       "name"  varchar(100),
//       "email" varchar(255) UNIQUE
//   );
//
// NOTE: Beego uses singular table names by default ("user", not "users").
// Set orm.SetDefaultTableNamePrefix or use `orm:"table(users)"` to override.

// ─────────────────────────────────────────────────────────────────
// Beego limitations vs migrate-gen
// ─────────────────────────────────────────────────────────────────
//
// Beego RunSyncdb is "additive only" — it never drops columns.
// This makes it unsuitable for production schema management.
// After running this adapter once to generate schema.sql, stop using
// RunSyncdb and let migrate-gen manage all future changes.

// suppress unused import warning in non-beego builds
var _ = os.DevNull
