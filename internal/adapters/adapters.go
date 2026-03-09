// internal/adapters/adapters.go
//
// Framework-specific schema dumpers.
// Each adapter converts its framework's "source of truth"
// (Go structs, ORM models, etc.) into a schema.sql file that
// migrate-gen can then process with the Shadow DB engine.
//
// The migrate-gen CORE never imports any of these.
// They are optional entry-points for teams migrating FROM an ORM.
//
// Usage:
//   go run ./cmd/migrate-gen dump --adapter=gorm > internal/schema/schema.sql
//   go run ./cmd/migrate-gen dump --adapter=bun  > internal/schema/schema.sql

package adapters

import "fmt"

// Adapter is the interface any framework dumper must implement.
type Adapter interface {
	// Name returns a short identifier, e.g. "gorm", "bun", "beego".
	Name() string
	// DumpSchema connects to a live DB (populated by the framework's
	// auto-migrator) and returns the CREATE TABLE SQL for all tables.
	DumpSchema(dsn string) (string, error)
}

var registry = map[string]Adapter{}

// Register makes an adapter available by name.
func Register(a Adapter) { registry[a.Name()] = a }

// Get returns the named adapter or an error.
func Get(name string) (Adapter, error) {
	a, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown adapter %q — available: gorm, bun, beego, bob, sqlboiler, pgx", name)
	}
	return a, nil
}
