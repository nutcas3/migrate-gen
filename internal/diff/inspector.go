// internal/diff/inspector.go
//
// Reads the complete schema of a live Postgres database using only
// standard INFORMATION_SCHEMA views + pg_indexes.
// No ORM. No third-party schema library. Pure SQL.

package diff

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/nutcas3/migrate-gen/models"
)

// scopedToPublic — all queries filter to schema='public', table_type='BASE TABLE'
// so we never accidentally diff system catalogs or views.

const sqlColumns = `
SELECT
    c.table_name,
    c.column_name,
    c.ordinal_position,
    c.data_type,
    c.character_maximum_length,
    c.numeric_precision,
    c.numeric_scale,
    c.datetime_precision,
    c.is_nullable,
    c.column_default,
    c.is_identity
FROM information_schema.columns c
JOIN information_schema.tables t
    ON  t.table_name   = c.table_name
    AND t.table_schema = c.table_schema
WHERE c.table_schema = 'public'
  AND t.table_type   = 'BASE TABLE'
ORDER BY c.table_name, c.ordinal_position;
`

const sqlPrimaryKeys = `
SELECT
    tc.table_name,
    kcu.column_name,
    kcu.ordinal_position
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage  kcu
    ON  kcu.constraint_name = tc.constraint_name
    AND kcu.table_schema    = tc.table_schema
WHERE tc.constraint_type = 'PRIMARY KEY'
  AND tc.table_schema    = 'public'
ORDER BY tc.table_name, kcu.ordinal_position;
`

const sqlForeignKeys = `
SELECT
    tc.constraint_name,
    kcu.table_name,
    kcu.column_name,
    ccu.table_name  AS ref_table,
    ccu.column_name AS ref_column,
    rc.delete_rule
FROM information_schema.table_constraints       tc
JOIN information_schema.key_column_usage        kcu
    ON  kcu.constraint_name = tc.constraint_name
    AND kcu.table_schema    = tc.table_schema
JOIN information_schema.constraint_column_usage ccu
    ON  ccu.constraint_name = tc.constraint_name
    AND ccu.table_schema    = tc.table_schema
JOIN information_schema.referential_constraints rc
    ON  rc.constraint_name   = tc.constraint_name
    AND rc.constraint_schema = tc.table_schema
WHERE tc.constraint_type = 'FOREIGN KEY'
  AND tc.table_schema    = 'public'
ORDER BY tc.table_name, kcu.column_name;
`

const sqlUniques = `
SELECT
    tc.constraint_name,
    tc.table_name,
    kcu.column_name,
    kcu.ordinal_position
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage  kcu
    ON  kcu.constraint_name = tc.constraint_name
    AND kcu.table_schema    = tc.table_schema
WHERE tc.constraint_type = 'UNIQUE'
  AND tc.table_schema    = 'public'
ORDER BY tc.table_name, tc.constraint_name, kcu.ordinal_position;
`

// pg_indexes is used instead of INFORMATION_SCHEMA because the standard
// view does not expose the index method (btree/hash/gin) or expression indexes.
const sqlIndexes = `
SELECT
    ix.indexname,
    ix.tablename,
    ix.indexdef,
    am.amname            AS method,
    i.indisunique        AS is_unique
FROM pg_indexes ix
JOIN pg_class   c  ON c.relname   = ix.indexname
JOIN pg_am      am ON am.oid      = c.relam
JOIN pg_index   i  ON i.indexrelid = c.oid
WHERE ix.schemaname = 'public'
  -- exclude PK indexes (already captured in sqlPrimaryKeys)
  AND ix.indexname NOT IN (
      SELECT constraint_name
      FROM information_schema.table_constraints
      WHERE constraint_type = 'PRIMARY KEY'
        AND table_schema    = 'public'
  )
ORDER BY ix.tablename, ix.indexname;
`

// InspectDB connects to dsn and returns the full schema of the public schema.
// db must be a *sql.DB (satisfied by pgx stdlib bridge, GORM db.DB(), Bun db.DB, etc.)
func InspectDB(ctx context.Context, db *sql.DB) (*models.Schema, error) {
	s := models.NewSchema()

	if err := loadColumns(ctx, db, s); err != nil {
		return nil, fmt.Errorf("columns: %w", err)
	}
	if err := loadPrimaryKeys(ctx, db, s); err != nil {
		return nil, fmt.Errorf("primary keys: %w", err)
	}
	if err := loadForeignKeys(ctx, db, s); err != nil {
		return nil, fmt.Errorf("foreign keys: %w", err)
	}
	if err := loadUniques(ctx, db, s); err != nil {
		return nil, fmt.Errorf("unique constraints: %w", err)
	}
	if err := loadIndexes(ctx, db, s); err != nil {
		return nil, fmt.Errorf("indexes: %w", err)
	}
	return s, nil
}

func loadColumns(ctx context.Context, db *sql.DB, s *models.Schema) error {
	rows, err := db.QueryContext(ctx, sqlColumns)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			tableName, colName, dataType string
			pos                          int
			charMaxLen                   sql.NullInt64
			numPrec, numScale            sql.NullInt64
			dtPrec                       sql.NullInt64
			isNullable, isIdentity       string
			colDefault                   sql.NullString
		)
		if err := rows.Scan(
			&tableName, &colName, &pos, &dataType,
			&charMaxLen, &numPrec, &numScale, &dtPrec,
			&isNullable, &colDefault, &isIdentity,
		); err != nil {
			return err
		}

		tbl := getOrCreateTable(s, tableName)
		col := &models.Column{
			Name:         colName,
			Position:     pos,
			RawType:      dataType,
			FullType:     normaliseType(dataType, charMaxLen, numPrec, numScale, dtPrec),
			IsNullable:   isNullable == "YES",
			DefaultValue: colDefault,
			IsIdentity:   isIdentity == "YES" || isIdentity == "ALWAYS",
		}
		tbl.Columns[colName] = col
		tbl.ColOrder = append(tbl.ColOrder, colName)
	}
	return rows.Err()
}

func loadPrimaryKeys(ctx context.Context, db *sql.DB, s *models.Schema) error {
	rows, err := db.QueryContext(ctx, sqlPrimaryKeys)
	if err != nil {
		return err
	}
	defer rows.Close()

	type pkRow struct {
		table, col string
		pos        int
	}
	pks := make(map[string][]pkRow)
	for rows.Next() {
		var r pkRow
		if err := rows.Scan(&r.table, &r.col, &r.pos); err != nil {
			return err
		}
		pks[r.table] = append(pks[r.table], r)
	}
	for tname, cols := range pks {
		tbl := getOrCreateTable(s, tname)
		sort.Slice(cols, func(i, j int) bool { return cols[i].pos < cols[j].pos })
		for _, c := range cols {
			tbl.PrimaryKeys = append(tbl.PrimaryKeys, c.col)
		}
	}
	return rows.Err()
}

func loadForeignKeys(ctx context.Context, db *sql.DB, s *models.Schema) error {
	rows, err := db.QueryContext(ctx, sqlForeignKeys)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var fk models.ForeignKey
		var tableName string
		if err := rows.Scan(
			&fk.ConstraintName, &tableName, &fk.Column,
			&fk.RefTable, &fk.RefColumn, &fk.OnDelete,
		); err != nil {
			return err
		}
		tbl := getOrCreateTable(s, tableName)
		tbl.ForeignKeys = append(tbl.ForeignKeys, &fk)
	}
	return rows.Err()
}

func loadUniques(ctx context.Context, db *sql.DB, s *models.Schema) error {
	rows, err := db.QueryContext(ctx, sqlUniques)
	if err != nil {
		return err
	}
	defer rows.Close()

	type urow struct {
		constraint, table, col string
		pos                    int
	}
	byConstraint := make(map[string][]urow)
	for rows.Next() {
		var r urow
		if err := rows.Scan(&r.constraint, &r.table, &r.col, &r.pos); err != nil {
			return err
		}
		byConstraint[r.constraint] = append(byConstraint[r.constraint], r)
	}
	for _, cols := range byConstraint {
		sort.Slice(cols, func(i, j int) bool { return cols[i].pos < cols[j].pos })
		tbl := getOrCreateTable(s, cols[0].table)
		var colNames []string
		for _, c := range cols {
			colNames = append(colNames, c.col)
		}
		tbl.Uniques = append(tbl.Uniques, colNames)
	}
	return rows.Err()
}

func loadIndexes(ctx context.Context, db *sql.DB, s *models.Schema) error {
	rows, err := db.QueryContext(ctx, sqlIndexes)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var idx models.Index
		var indexDef string
		if err := rows.Scan(&idx.Name, &idx.TableName, &indexDef, &idx.Method, &idx.IsUnique); err != nil {
			return err
		}
		idx.Columns = parseIndexColumns(indexDef)
		s.Indexes[idx.Name] = &idx
	}
	return rows.Err()
}

func getOrCreateTable(s *models.Schema, name string) *models.Table {
	if t, ok := s.Tables[name]; ok {
		return t
	}
	t := &models.Table{Name: name, Columns: make(map[string]*models.Column)}
	s.Tables[name] = t
	return t
}

// normaliseType converts INFORMATION_SCHEMA's split representation
// (data_type + separate length/precision columns) back into one string.
// e.g. data_type="character varying", charMaxLen=255 → "varchar(255)"
func normaliseType(dt string, charLen, numPrec, numScale, dtPrec sql.NullInt64) string {
	switch strings.ToLower(dt) {
	case "character varying":
		if charLen.Valid {
			return fmt.Sprintf("varchar(%d)", charLen.Int64)
		}
		return "varchar"
	case "character":
		if charLen.Valid {
			return fmt.Sprintf("char(%d)", charLen.Int64)
		}
		return "char"
	case "numeric", "decimal":
		if numPrec.Valid && numScale.Valid {
			return fmt.Sprintf("%s(%d,%d)", dt, numPrec.Int64, numScale.Int64)
		}
		return dt
	case "time without time zone":
		return "time"
	case "time with time zone":
		return "timetz"
	case "timestamp without time zone":
		if dtPrec.Valid && dtPrec.Int64 != 6 {
			return fmt.Sprintf("timestamp(%d)", dtPrec.Int64)
		}
		return "timestamp"
	case "timestamp with time zone":
		if dtPrec.Valid && dtPrec.Int64 != 6 {
			return fmt.Sprintf("timestamptz(%d)", dtPrec.Int64)
		}
		return "timestamptz"
	case "integer":
		return "int"
	case "bigint":
		return "bigint"
	case "boolean":
		return "boolean"
	case "text":
		return "text"
	case "jsonb":
		return "jsonb"
	case "json":
		return "json"
	case "uuid":
		return "uuid"
	default:
		return dt
	}
}

// parseIndexColumns extracts column names from pg_indexes.indexdef.
// indexdef format: "CREATE [UNIQUE] INDEX name ON table USING method (col1, col2)"
func parseIndexColumns(def string) []string {
	start := strings.LastIndex(def, "(")
	end := strings.LastIndex(def, ")")
	if start == -1 || end == -1 || start >= end {
		return nil
	}
	raw := def[start+1 : end]
	parts := strings.Split(raw, ",")
	var cols []string
	for _, p := range parts {
		cols = append(cols, strings.TrimSpace(p))
	}
	return cols
}
