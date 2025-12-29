package analytics

import (
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"os"
)

//go:embed templates/*
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// StaticFS returns the embedded static files filesystem.
func StaticFS() embed.FS {
	return staticFS
}

// App holds application dependencies for the flow analytics service.
type App struct {
	querier      Querier
	queryBuilder *QueryBuilder
	resultParser *ResultParser
	columns      *ColumnRegistry
	templates    *template.Template
	tableName    string
	logger       *slog.Logger
}

// AppOption configures the App.
type AppOption func(*App)

// WithQuerier sets the ClickHouse querier.
func WithQuerier(q Querier) AppOption {
	return func(a *App) {
		a.querier = q
	}
}

// WithTableName sets the flows table name.
func WithTableName(name string) AppOption {
	return func(a *App) {
		a.tableName = name
	}
}

// WithAppLogger sets the logger.
func WithAppLogger(logger *slog.Logger) AppOption {
	return func(a *App) {
		a.logger = logger
	}
}

// NewApp creates a new App with the given options.
func NewApp(opts ...AppOption) (*App, error) {
	// Parse templates
	tmpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	columns := NewColumnRegistry(DefaultColumns())

	app := &App{
		templates:    tmpl,
		columns:      columns,
		queryBuilder: NewQueryBuilder(columns),
	}

	for _, opt := range opts {
		opt(app)
	}

	// Set defaults
	if app.logger == nil {
		app.logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	// Initialize result parser with logger (after logger is set)
	app.resultParser = NewResultParser(app.logger)

	if app.tableName == "" {
		app.tableName = defaultTableName
	}

	// Validate required dependencies
	if app.querier == nil {
		return nil, fmt.Errorf("querier is required: use WithQuerier option")
	}

	return app, nil
}

// Querier returns the ClickHouse querier.
func (a *App) Querier() Querier {
	return a.querier
}

// QueryBuilder returns the query builder.
func (a *App) QueryBuilder() *QueryBuilder {
	return a.queryBuilder
}

// ResultParser returns the result parser.
func (a *App) ResultParser() *ResultParser {
	return a.resultParser
}

// Columns returns the column registry.
func (a *App) Columns() *ColumnRegistry {
	return a.columns
}

// Templates returns the parsed templates.
func (a *App) Templates() *template.Template {
	return a.templates
}

// TableName returns the flows table name.
func (a *App) TableName() string {
	return a.tableName
}

// Logger returns the logger.
func (a *App) Logger() *slog.Logger {
	return a.logger
}
