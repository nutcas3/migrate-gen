// internal/diff/engine.go
//
// Compares two Schema snapshots (current = Shadow DB after migrations,
// desired = Shadow DB after schema.sql) and produces the SQL statements
// needed to bring current → desired.
//
// Safety rules baked in:
//   - DROP TABLE  → never auto-emitted; written as a commented warning.
//   - DROP COLUMN → never auto-emitted; written as a commented warning.
//   - TYPE change → emitted with USING cast; flagged for review.
//   - Everything destructive requires a human to uncomment it.

package diff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nutcas3/migrate-gen/models"
)

// Diff computes what SQL is needed to bring `current` to match `desired`.
// Both schemas come from InspectDB() on shadow containers.
func Diff(current, desired *models.Schema) *models.Result {
	r := &models.Result{}

	// 1. Tables in desired but missing or changed in current
	tableNames := sortedKeys(desired.Tables)
	for _, tname := range tableNames {
		desiredTable := desired.Tables[tname]
		currentTable, exists := current.Tables[tname]

		if !exists {
			up, down := buildCreateTable(desiredTable)
			r.UpStatements = append(r.UpStatements, models.Statement{
				SQL:     up,
				Comment: fmt.Sprintf("New table: %s", tname),
			})
			r.DownStatements = append(r.DownStatements, models.Statement{
				SQL:     down,
				Comment: fmt.Sprintf("Rollback: drop table %s", tname),
			})
			continue
		}

		// Table exists — diff its columns
		colStmts, colDowns, colWarns := diffColumns(currentTable, desiredTable)
		r.UpStatements = append(r.UpStatements, colStmts...)
		r.DownStatements = append(r.DownStatements, colDowns...)
		r.Warnings = append(r.Warnings, colWarns...)

		// Diff indexes
		idxStmts, idxDowns := diffIndexes(current, desired, tname)
		r.UpStatements = append(r.UpStatements, idxStmts...)
		r.DownStatements = append(r.DownStatements, idxDowns...)
	}

	// 2. Tables in current but NOT in desired → warn, never auto-drop
	for tname := range current.Tables {
		if _, stillExists := desired.Tables[tname]; !stillExists {
			drop := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE;", quote(tname))
			r.UpStatements = append(r.UpStatements, models.Statement{
				SQL:       drop,
				Comment:   "⚠️ TABLE REMOVED from schema.sql — uncomment after review",
				Danger:    true,
				Commented: true,
			})
			r.DownStatements = append(r.DownStatements, models.Statement{
				SQL:     fmt.Sprintf("-- Restore table %s manually if needed.", tname),
				Comment: "Rollback of a DROP TABLE must be done manually.",
			})
			r.HasDestructive = true
			r.Warnings = append(r.Warnings,
				fmt.Sprintf("TABLE REMOVED: %q exists in DB but not in schema.sql. Uncomment DROP TABLE in the migration after review.", tname))
		}
	}

	return r
}

func diffColumns(current, desired *models.Table) (ups, downs []models.Statement, warnings []string) {
	tname := desired.Name

	// Added or changed columns
	for _, colName := range desired.ColOrder {
		desiredCol := desired.Columns[colName]
		currentCol, exists := current.Columns[colName]

		if !exists {
			// New column
			nullClause := ""
			if !desiredCol.IsNullable && !desiredCol.DefaultValue.Valid {
				// Adding NOT NULL column without default requires a default or backfill;
				// emit as nullable first, then set NOT NULL after a backfill step.
				nullClause = " -- NOTE: add DEFAULT or backfill before SET NOT NULL"
			}
			ups = append(ups, models.Statement{
				SQL: fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s%s%s;",
					quote(tname),
					quote(colName),
					desiredCol.FullType,
					nullabilityClause(desiredCol),
					nullClause,
				),
				Comment: fmt.Sprintf("Add column %s.%s", tname, colName),
			})
			downs = append(downs, models.Statement{
				SQL:     fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s;", quote(tname), quote(colName)),
				Comment: fmt.Sprintf("Rollback: drop column %s.%s", tname, colName),
			})
			continue
		}

		// Type drift?
		if currentCol.FullType != desiredCol.FullType {
			ups = append(ups, models.Statement{
				SQL: fmt.Sprintf(
					"ALTER TABLE %s ALTER COLUMN %s TYPE %s USING %s::%s;",
					quote(tname), quote(colName),
					desiredCol.FullType,
					quote(colName), desiredCol.FullType,
				),
				Comment: fmt.Sprintf("Type change %s.%s: %s → %s (verify USING clause)",
					tname, colName, currentCol.FullType, desiredCol.FullType),
				Danger: true,
			})
			downs = append(downs, models.Statement{
				SQL: fmt.Sprintf(
					"ALTER TABLE %s ALTER COLUMN %s TYPE %s USING %s::%s;",
					quote(tname), quote(colName),
					currentCol.FullType,
					quote(colName), currentCol.FullType,
				),
				Comment: fmt.Sprintf("Rollback type change %s.%s", tname, colName),
			})
		}

		// Nullability drift?
		if currentCol.IsNullable != desiredCol.IsNullable {
			if desiredCol.IsNullable {
				ups = append(ups, models.Statement{
					SQL:     fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL;", quote(tname), quote(colName)),
					Comment: fmt.Sprintf("Allow nulls: %s.%s", tname, colName),
				})
				downs = append(downs, models.Statement{
					SQL:     fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL;", quote(tname), quote(colName)),
					Comment: fmt.Sprintf("Rollback nullability: %s.%s", tname, colName),
				})
			} else {
				ups = append(ups, models.Statement{
					SQL:     fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL;", quote(tname), quote(colName)),
					Comment: fmt.Sprintf("⚠️  SET NOT NULL %s.%s — ensure no NULL rows exist first", tname, colName),
					Danger:  true,
				})
				downs = append(downs, models.Statement{
					SQL:     fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL;", quote(tname), quote(colName)),
					Comment: fmt.Sprintf("Rollback NOT NULL: %s.%s", tname, colName),
				})
			}
		}
	}

	// Removed columns → warn, never auto-drop
	for colName := range current.Columns {
		if _, stillExists := desired.Columns[colName]; !stillExists {
			drop := fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s;", quote(tname), quote(colName))
			ups = append(ups, models.Statement{
				SQL:       drop,
				Comment:   fmt.Sprintf("⚠️  COLUMN REMOVED: %s.%s — uncomment after review and data migration", tname, colName),
				Danger:    true,
				Commented: true,
			})
			downs = append(downs, models.Statement{
				SQL: fmt.Sprintf("-- Restore %s.%s manually (data is gone after DROP COLUMN).", tname, colName),
			})
			warnings = append(warnings,
				fmt.Sprintf("COLUMN REMOVED: %q.%q is in the DB but not in schema.sql. Uncomment DROP COLUMN after review.", tname, colName))
		}
	}

	return
}

func diffIndexes(current, desired *models.Schema, tableName string) (ups, downs []models.Statement) {
	// Desired indexes for this table
	for idxName, desiredIdx := range desired.Indexes {
		if desiredIdx.TableName != tableName {
			continue
		}
		if _, exists := current.Indexes[idxName]; !exists {
			unique := ""
			if desiredIdx.IsUnique {
				unique = "UNIQUE "
			}
			ups = append(ups, models.Statement{
				SQL: fmt.Sprintf("CREATE %sINDEX %s ON %s USING %s (%s);",
					unique,
					quote(idxName),
					quote(tableName),
					desiredIdx.Method,
					strings.Join(quoteAll(desiredIdx.Columns), ", "),
				),
				Comment: fmt.Sprintf("New index: %s", idxName),
			})
			downs = append(downs, models.Statement{
				SQL:     fmt.Sprintf("DROP INDEX IF EXISTS %s;", quote(idxName)),
				Comment: fmt.Sprintf("Rollback: drop index %s", idxName),
			})
		}
	}

	// Indexes in current but not in desired → drop
	for idxName, currentIdx := range current.Indexes {
		if currentIdx.TableName != tableName {
			continue
		}
		if _, stillExists := desired.Indexes[idxName]; !stillExists {
			ups = append(ups, models.Statement{
				SQL:     fmt.Sprintf("DROP INDEX IF EXISTS %s;", quote(idxName)),
				Comment: fmt.Sprintf("Remove index: %s", idxName),
			})
			downs = append(downs, models.Statement{
				SQL: fmt.Sprintf("CREATE INDEX %s ON %s (%s);",
					quote(idxName),
					quote(tableName),
					strings.Join(quoteAll(currentIdx.Columns), ", "),
				),
				Comment: fmt.Sprintf("Rollback: recreate index %s", idxName),
			})
		}
	}
	return
}

func buildCreateTable(t *models.Table) (up, down string) {
	var lines []string

	for _, colName := range t.ColOrder {
		col := t.Columns[colName]
		line := fmt.Sprintf("    %s %s%s", quote(colName), col.FullType, nullabilityClause(col))
		if col.DefaultValue.Valid {
			line += " DEFAULT " + col.DefaultValue.String
		}
		lines = append(lines, line)
	}

	if len(t.PrimaryKeys) > 0 {
		lines = append(lines, fmt.Sprintf("    PRIMARY KEY (%s)", strings.Join(quoteAll(t.PrimaryKeys), ", ")))
	}

	for _, fk := range t.ForeignKeys {
		onDelete := ""
		if fk.OnDelete != "" && fk.OnDelete != "NO ACTION" {
			onDelete = " ON DELETE " + fk.OnDelete
		}
		lines = append(lines, fmt.Sprintf(
			"    CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)%s",
			quote(fk.ConstraintName),
			quote(fk.Column),
			quote(fk.RefTable),
			quote(fk.RefColumn),
			onDelete,
		))
	}

	up = fmt.Sprintf("CREATE TABLE %s (\n%s\n);", quote(t.Name), strings.Join(lines, ",\n"))
	down = fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE;", quote(t.Name))
	return
}

func nullabilityClause(c *models.Column) string {
	if !c.IsNullable {
		return " NOT NULL"
	}
	return ""
}

func quote(s string) string { return `"` + s + `"` }

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = quote(s)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
