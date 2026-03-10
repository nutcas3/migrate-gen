// Package adapters provides framework-specific schema dumpers.
//
// Each adapter converts its framework's "source of truth"
// (Go structs, ORM models, etc.) into a schema.sql file that
// migrate-gen can then process with the Shadow DB engine.
//
// Usage:
//
//	adapter, err := adapters.Get("gorm")
//	schema, err := adapter.DumpSchema(dsn)
package adapters

import (
	"github.com/nutcas3/migrate-gen/internal/adapters"
)

// Adapter is the interface any framework dumper must implement.
type Adapter = adapters.Adapter

// Register makes an adapter available by name.
func Register(a Adapter) {
	adapters.Register(a)
}

// Get returns the named adapter or an error.
func Get(name string) (Adapter, error) {
	return adapters.Get(name)
}
