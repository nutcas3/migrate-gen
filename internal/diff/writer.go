// internal/diff/writer.go
//
// Writes golang-migrate compatible *.up.sql and *.down.sql files
// from a diff.Result. Also handles the CI verification mode (--check).

package diff

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nutcas3/migrate-gen/models"
)

// WriteMigration writes the .up.sql and .down.sql files to MigrationsDir.
// Returns the paths of files written, or empty slice if nothing changed.
func WriteMigration(result *models.Result, opts models.WriteOptions) ([]string, error) {
	if result.IsEmpty() {
		return nil, nil
	}

	if err := os.MkdirAll(opts.MigrationsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create migrations dir: %w", err)
	}

	seq := nextSequence(opts.MigrationsDir)
	name := sanitize(opts.Name)
	if name == "" {
		name = "schema_update"
	}

	base := fmt.Sprintf("%06d_%s", seq, name)
	upPath := filepath.Join(opts.MigrationsDir, base+".up.sql")
	downPath := filepath.Join(opts.MigrationsDir, base+".down.sql")

	if err := writeFile(upPath, result.UpStatements, "UP", opts.Name); err != nil {
		return nil, err
	}
	if err := writeFile(downPath, result.DownStatements, "DOWN", opts.Name); err != nil {
		return nil, err
	}

	return []string{upPath, downPath}, nil
}

func writeFile(path string, stmts []models.Statement, direction, migName string) error {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("-- Migration: %s (%s)\n", migName, direction))
	sb.WriteString(fmt.Sprintf("-- Generated: %s by migrate-gen\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString("-- DO NOT EDIT manually. Re-run `make gen` to regenerate.\n\n")

	hasDanger := false
	for _, s := range stmts {
		if s.Danger {
			hasDanger = true
			break
		}
	}
	if hasDanger {
		sb.WriteString("-- ⚠️  WARNING: This migration contains flagged statements.\n")
		sb.WriteString("-- Statements marked with ⚠️  require senior-engineer review before applying.\n\n")
	}

	for _, stmt := range stmts {
		if stmt.Comment != "" {
			sb.WriteString("-- " + stmt.Comment + "\n")
		}
		if stmt.Commented {
			// Emit as commented-out SQL — human must explicitly uncomment
			sb.WriteString("-- " + stmt.SQL + "\n\n")
		} else {
			sb.WriteString(stmt.SQL + "\n\n")
		}
	}

	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// FormatCheckOutput renders a summary for CI log output.
func FormatCheckOutput(result *models.Result) *models.CheckResult {
	if result.IsEmpty() {
		return &models.CheckResult{InSync: true}
	}

	var changes []string
	for _, s := range result.UpStatements {
		if s.Comment != "" {
			changes = append(changes, "  • "+s.Comment)
		} else {
			preview := s.SQL
			if len(preview) > 80 {
				preview = preview[:80] + "…"
			}
			changes = append(changes, "  • "+preview)
		}
	}
	return &models.CheckResult{InSync: false, Changes: changes}
}

// nextSequence returns the next integer after the highest existing sequence
// number in migrationsDir. Returns 1 if none exist.
func nextSequence(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 1
	}
	var nums []int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			parts := strings.SplitN(e.Name(), "_", 2)
			if n, err := strconv.Atoi(parts[0]); err == nil {
				nums = append(nums, n)
			}
		}
	}
	if len(nums) == 0 {
		return 1
	}
	sort.Ints(nums)
	return nums[len(nums)-1] + 1
}

// sanitize turns a migration name into a safe filename component.
func sanitize(s string) string {
	r := strings.NewReplacer(
		" ", "_", "-", "_", "/", "_",
		"\\", "_", ".", "_", ":", "_",
	)
	return strings.ToLower(strings.TrimSpace(r.Replace(s)))
}
