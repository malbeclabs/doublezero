package neo4j

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const DefaultDatabase = "neo4j"

// Client represents a Neo4j database connection.
type Client interface {
	Session(ctx context.Context) (Session, error)
	Close(ctx context.Context) error
}

// Session represents a Neo4j session for executing queries.
type Session interface {
	Run(ctx context.Context, cypher string, params map[string]any) (Result, error)
	ExecuteRead(ctx context.Context, work TransactionWork) (any, error)
	ExecuteWrite(ctx context.Context, work TransactionWork) (any, error)
	Close(ctx context.Context) error
}

// TransactionWork is a function that runs within a transaction.
type TransactionWork func(tx Transaction) (any, error)

// Transaction represents a Neo4j transaction.
type Transaction interface {
	Run(ctx context.Context, cypher string, params map[string]any) (Result, error)
}

// Result represents the result of a Neo4j query.
type Result interface {
	Next(ctx context.Context) bool
	Record() *neo4j.Record
	Err() error
	Consume(ctx context.Context) (neo4j.ResultSummary, error)
	Collect(ctx context.Context) ([]*neo4j.Record, error)
	Single(ctx context.Context) (*neo4j.Record, error)
}

type client struct {
	driver   neo4j.DriverWithContext
	database string
	log      *slog.Logger
}

type session struct {
	sess     neo4j.SessionWithContext
	database string
}

type transaction struct {
	tx neo4j.ManagedTransaction
}

type result struct {
	res neo4j.ResultWithContext
}

// NewClient creates a new Neo4j client.
func NewClient(ctx context.Context, log *slog.Logger, uri, database, username, password string) (Client, error) {
	auth := neo4j.BasicAuth(username, password, "")
	driver, err := neo4j.NewDriverWithContext(uri, auth)
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j driver: %w", err)
	}

	if err := driver.VerifyConnectivity(ctx); err != nil {
		driver.Close(ctx)
		return nil, fmt.Errorf("failed to verify Neo4j connectivity: %w", err)
	}

	log.Info("Neo4j client initialized", "uri", uri, "database", database)

	return &client{
		driver:   driver,
		database: database,
		log:      log,
	}, nil
}

func (c *client) Session(ctx context.Context) (Session, error) {
	sess := c.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: c.database,
	})
	return &session{sess: sess, database: c.database}, nil
}

func (c *client) Close(ctx context.Context) error {
	return c.driver.Close(ctx)
}

func (s *session) Run(ctx context.Context, cypher string, params map[string]any) (Result, error) {
	res, err := s.sess.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	return &result{res: res}, nil
}

func (s *session) ExecuteRead(ctx context.Context, work TransactionWork) (any, error) {
	return s.sess.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		return work(&transaction{tx: tx})
	})
}

func (s *session) ExecuteWrite(ctx context.Context, work TransactionWork) (any, error) {
	return s.sess.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		return work(&transaction{tx: tx})
	})
}

func (s *session) Close(ctx context.Context) error {
	return s.sess.Close(ctx)
}

func (t *transaction) Run(ctx context.Context, cypher string, params map[string]any) (Result, error) {
	res, err := t.tx.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	return &result{res: res}, nil
}

func (r *result) Next(ctx context.Context) bool {
	return r.res.Next(ctx)
}

func (r *result) Record() *neo4j.Record {
	return r.res.Record()
}

func (r *result) Err() error {
	return r.res.Err()
}

func (r *result) Consume(ctx context.Context) (neo4j.ResultSummary, error) {
	return r.res.Consume(ctx)
}

func (r *result) Collect(ctx context.Context) ([]*neo4j.Record, error) {
	return r.res.Collect(ctx)
}

func (r *result) Single(ctx context.Context) (*neo4j.Record, error) {
	return r.res.Single(ctx)
}
