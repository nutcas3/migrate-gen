package models

import "database/sql"

type Schema struct {
	Tables  map[string]*Table
	Indexes map[string]*Index
}

func NewSchema() *Schema {
	return &Schema{Tables: make(map[string]*Table), Indexes: make(map[string]*Index)}
}

type Table struct {
	Name        string
	Columns     map[string]*Column
	ColOrder    []string // ordinal order — preserved when writing CREATE TABLE
	PrimaryKeys []string // ordered column names
	ForeignKeys []*ForeignKey
	Uniques     [][]string // each inner slice is one unique constraint
}

type Column struct {
	Name         string
	Position     int
	RawType      string // raw data_type from INFORMATION_SCHEMA
	FullType     string // normalised: varchar(255), numeric(10,2), etc.
	IsNullable   bool
	DefaultValue sql.NullString
	IsIdentity   bool // GENERATED ALWAYS AS IDENTITY / SERIAL
}

type ForeignKey struct {
	ConstraintName string
	Column         string
	RefTable       string
	RefColumn      string
	OnDelete       string // CASCADE | SET NULL | RESTRICT | NO ACTION
}

type Index struct {
	Name      string
	TableName string
	IsUnique  bool
	Columns   []string
	Method    string // btree | hash | gin | gist | brin
}
