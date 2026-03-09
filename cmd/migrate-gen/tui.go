// cmd/migrate-gen/tui.go
//
// Interactive TUI for migrate-gen using Bubbletea and Charm

package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	migrate_gen "github.com/nutcas3/migrate-gen"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1).
			Bold(true)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A6E3A1")).
			PaddingLeft(1)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F38BA8")).
			PaddingLeft(1)

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F9E2AF")).
			PaddingLeft(1)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#89B4FA")).
			PaddingLeft(1)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A6E3A1")).
			Bold(true)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#6C7086")).
			Padding(1, 2).
			Margin(1, 0)

	progressStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A6E3A1")).
			Width(40)
)

// State represents the current operation state
type State int

const (
	StateIdle State = iota
	StateStartingCurrent
	StateApplyingMigrations
	StateInspectingCurrent
	StateStartingDesired
	StateApplyingSchema
	StateInspectingDesired
	StateComputingDiff
	StateWritingMigration
	StateComplete
	StateError
)

// Model is the Bubbletea model for our TUI
type Model struct {
	spinner  spinner.Model
	progress progress.Model
	state    State
	config   config
	err      error

	// Operation data
	currentContainer *migrate_gen.Container
	desiredContainer *migrate_gen.Container
	result           *migrate_gen.Result
	migrationName    string
	writtenFiles     []string
	warnings         []string

	// Context for operations
	ctx context.Context
}

// InitialModel creates the initial TUI model
func InitialModel(cfg config, migrationName string) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))

	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(40),
	)

	return Model{
		spinner:       s,
		progress:      p,
		state:         StateIdle,
		config:        cfg,
		migrationName: migrationName,
		ctx:           context.Background(),
	}
}

// Init initializes the Bubbletea model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		startOperation,
	)
}

// Update handles Bubbletea updates
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progress.FrameMsg:
		var cmd tea.Cmd
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd

	case operationStartedMsg:
		m.state = StateStartingCurrent
		return m, tea.Batch(
			m.startCurrentContainer(),
			m.spinner.Tick,
		)

	case currentContainerStartedMsg:
		m.state = StateApplyingMigrations
		return m, tea.Batch(
			m.applyMigrations(),
			m.spinner.Tick,
		)

	case migrationsAppliedMsg:
		m.state = StateInspectingCurrent
		return m, tea.Batch(
			m.inspectCurrentSchema(),
			m.spinner.Tick,
		)

	case currentSchemaInspectedMsg:
		m.state = StateStartingDesired
		return m, tea.Batch(
			m.startDesiredContainer(),
			m.spinner.Tick,
		)

	case desiredContainerStartedMsg:
		m.state = StateApplyingSchema
		return m, tea.Batch(
			m.applySchema(),
			m.spinner.Tick,
		)

	case schemaAppliedMsg:
		m.state = StateInspectingDesired
		return m, tea.Batch(
			m.inspectDesiredSchema(),
			m.spinner.Tick,
		)

	case desiredSchemaInspectedMsg:
		m.state = StateComputingDiff
		return m, tea.Batch(
			m.computeDiff(),
			m.spinner.Tick,
		)

	case diffComputedMsg:
		m.state = StateWritingMigration
		return m, tea.Batch(
			m.writeMigration(),
			m.spinner.Tick,
		)

	case migrationWrittenMsg:
		m.state = StateComplete
		return m, tea.Quit

	case errorMsg:
		m.state = StateError
		m.err = msg.err
		return m, tea.Quit
	}

	// Handle spinner updates for ongoing operations
	if m.state != StateIdle && m.state != StateComplete && m.state != StateError {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(spinner.TickMsg{})
		return m, cmd
	}

	return m, nil
}

// View renders the TUI
func (m Model) View() string {
	if m.state == StateError {
		return m.renderError()
	}

	if m.state == StateComplete {
		return m.renderSuccess()
	}

	content := []string{
		titleStyle.Render("🚀 migrate-gen"),
		"",
		m.renderStatus(),
		m.renderProgress(),
	}

	if len(m.warnings) > 0 {
		content = append(content, "", m.renderWarnings())
	}

	return lipgloss.JoinVertical(lipgloss.Left, content...)
}

// renderStatus shows the current operation status
func (m Model) renderStatus() string {
	var status string
	var style lipgloss.Style

	switch m.state {
	case StateIdle:
		status = "Initializing..."
		style = infoStyle
	case StateStartingCurrent:
		status = "Starting shadow DB for current state..."
		style = infoStyle
	case StateApplyingMigrations:
		status = "Applying existing migrations..."
		style = infoStyle
	case StateInspectingCurrent:
		status = "Inspecting current schema..."
		style = infoStyle
	case StateStartingDesired:
		status = "Starting shadow DB for desired state..."
		style = infoStyle
	case StateApplyingSchema:
		status = "Applying schema.sql..."
		style = infoStyle
	case StateInspectingDesired:
		status = "Inspecting desired schema..."
		style = infoStyle
	case StateComputingDiff:
		status = "Computing schema diff..."
		style = infoStyle
	case StateWritingMigration:
		status = "Writing migration files..."
		style = infoStyle
	default:
		status = "Unknown state"
		style = infoStyle
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, m.spinner.View(), style.Render(status))
}

// renderProgress shows the progress bar
func (m Model) renderProgress() string {
	var progressPercent float64

	switch m.state {
	case StateIdle:
		progressPercent = 0.0
	case StateStartingCurrent:
		progressPercent = 0.1
	case StateApplyingMigrations:
		progressPercent = 0.25
	case StateInspectingCurrent:
		progressPercent = 0.4
	case StateStartingDesired:
		progressPercent = 0.5
	case StateApplyingSchema:
		progressPercent = 0.65
	case StateInspectingDesired:
		progressPercent = 0.8
	case StateComputingDiff:
		progressPercent = 0.9
	case StateWritingMigration:
		progressPercent = 0.95
	case StateComplete:
		progressPercent = 1.0
	default:
		progressPercent = 0.0
	}

	return fmt.Sprintf("Progress: %s", m.progress.ViewAs(progressPercent))
}

// renderWarnings shows any warnings
func (m Model) renderWarnings() string {
	warningBox := boxStyle.Copy().BorderForeground(lipgloss.Color("#F9E2AF"))
	content := []string{warningStyle.Render("⚠️  Warnings:")}

	for _, warning := range m.warnings {
		content = append(content, warningStyle.Render("• "+warning))
	}

	return warningBox.Render(lipgloss.JoinVertical(lipgloss.Left, content...))
}

// renderError shows the error state
func (m Model) renderError() string {
	errorBox := boxStyle.Copy().BorderForeground(lipgloss.Color("#F38BA8"))
	content := []string{
		errorStyle.Render("❌ Error occurred!"),
		"",
		errorStyle.Render(m.err.Error()),
		"",
		infoStyle.Render("Press Ctrl+C to exit"),
	}

	return errorBox.Render(lipgloss.JoinVertical(lipgloss.Left, content...))
}

// renderSuccess shows the success state
func (m Model) renderSuccess() string {
	successBox := boxStyle.Copy().BorderForeground(lipgloss.Color("#A6E3A1"))
	content := []string{
		successStyle.Render("✅ Migration generated successfully!"),
		"",
	}

	if len(m.writtenFiles) > 0 {
		content = append(content, infoStyle.Render("Files created:"))
		for _, file := range m.writtenFiles {
			content = append(content, statusStyle.Render("• "+file))
		}
	}

	if m.result != nil && m.result.HasDestructive {
		content = append(content, "", warningStyle.Render("⚠️  Destructive operations are commented out for review"))
	}

	content = append(content, "", infoStyle.Render("Press Ctrl+C to exit"))

	return successBox.Render(lipgloss.JoinVertical(lipgloss.Left, content...))
}

// Message types for tea.Cmd
type (
	operationStartedMsg        struct{}
	currentContainerStartedMsg struct{}
	migrationsAppliedMsg       struct{}
	currentSchemaInspectedMsg  struct{}
	desiredContainerStartedMsg struct{}
	schemaAppliedMsg           struct{}
	desiredSchemaInspectedMsg  struct{}
	diffComputedMsg            struct{}
	migrationWrittenMsg        struct{}
	errorMsg                   struct{ err error }
)

// Command functions
func startOperation() tea.Msg { return operationStartedMsg{} }

func (m Model) startCurrentContainer() tea.Cmd {
	return func() tea.Msg {
		container, err := migrate_gen.Start(m.ctx)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to start current container: %w", err)}
		}
		m.currentContainer = container
		return currentContainerStartedMsg{}
	}
}

func (m Model) applyMigrations() tea.Cmd {
	return func() tea.Msg {
		if err := m.currentContainer.ApplyMigrations(m.ctx, m.config.MigrationsDir); err != nil {
			return errorMsg{err: fmt.Errorf("failed to apply migrations: %w", err)}
		}
		return migrationsAppliedMsg{}
	}
}

func (m Model) inspectCurrentSchema() tea.Cmd {
	return func() tea.Msg {
		db, err := m.currentContainer.DB()
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to open current DB: %w", err)}
		}
		defer db.Close()

		_, err = migrate_gen.InspectDB(m.ctx, db)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to inspect current schema: %w", err)}
		}

		// Store schema for later use
		return currentSchemaInspectedMsg{}
	}
}

func (m Model) startDesiredContainer() tea.Cmd {
	return func() tea.Msg {
		container, err := migrate_gen.Start(m.ctx)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to start desired container: %w", err)}
		}
		m.desiredContainer = container
		return desiredContainerStartedMsg{}
	}
}

func (m Model) applySchema() tea.Cmd {
	return func() tea.Msg {
		if err := m.desiredContainer.ApplySchemaFile(m.ctx, m.config.SchemaFile); err != nil {
			return errorMsg{err: fmt.Errorf("failed to apply schema: %w", err)}
		}
		return schemaAppliedMsg{}
	}
}

func (m Model) inspectDesiredSchema() tea.Cmd {
	return func() tea.Msg {
		db, err := m.desiredContainer.DB()
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to open desired DB: %w", err)}
		}
		defer db.Close()

		_, err = migrate_gen.InspectDB(m.ctx, db)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to inspect desired schema: %w", err)}
		}

		return desiredSchemaInspectedMsg{}
	}
}

func (m Model) computeDiff() tea.Cmd {
	return func() tea.Msg {
		// Get schemas from both containers
		currentDB, err := m.currentContainer.DB()
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to open current DB for diff: %w", err)}
		}
		defer currentDB.Close()

		desiredDB, err := m.desiredContainer.DB()
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to open desired DB for diff: %w", err)}
		}
		defer desiredDB.Close()

		currentSchema, err := migrate_gen.InspectDB(m.ctx, currentDB)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to inspect current schema for diff: %w", err)}
		}

		desiredSchema, err := migrate_gen.InspectDB(m.ctx, desiredDB)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to inspect desired schema for diff: %w", err)}
		}

		result := migrate_gen.Diff(currentSchema, desiredSchema)
		m.result = result
		m.warnings = result.Warnings

		return diffComputedMsg{}
	}
}

func (m Model) writeMigration() tea.Cmd {
	return func() tea.Msg {
		if m.result.IsEmpty() {
			return errorMsg{err: fmt.Errorf("no schema changes detected")}
		}

		written, err := migrate_gen.WriteMigration(m.result, migrate_gen.WriteOptions{
			MigrationsDir: m.config.MigrationsDir,
			Name:          m.migrationName,
		})
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to write migration: %w", err)}
		}

		m.writtenFiles = written
		return migrationWrittenMsg{}
	}
}
