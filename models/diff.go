package models

type Result struct {
	UpStatements   []Statement
	DownStatements []Statement
	Warnings       []string
	HasDestructive bool // true if any warned DROP/TRUNCATE is present
}

type Statement struct {
	SQL      string
	Comment  string // displayed above the statement in the .sql file
	Danger   bool   // true = flagged for senior-engineer review
	Commented bool  // true = written as a SQL comment (DROP TABLE etc.)
}

func (r *Result) IsEmpty() bool {
	return len(r.UpStatements) == 0 && len(r.Warnings) == 0
}


type WriteOptions struct {
	MigrationsDir string
	Name          string // migration name, e.g. "add_posts_table"
}

type CheckResult struct {
	InSync  bool
	Changes []string // human-readable summary of what changed
}
