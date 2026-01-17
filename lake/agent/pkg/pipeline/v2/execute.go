package v2

import (
	"context"
	"strings"
	"sync"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
)

// Execute runs all queries in the plan and returns the results.
func (p *Pipeline) Execute(ctx context.Context, plan *QueryPlan) ([]pipeline.ExecutedQuery, error) {
	// Collect all queries to execute
	allQueries := make([]PlannedQuery, 0, len(plan.ValidationQueries)+len(plan.AnswerQueries))
	allQueries = append(allQueries, plan.ValidationQueries...)
	allQueries = append(allQueries, plan.AnswerQueries...)

	// Log queries for debugging
	for i, pq := range allQueries {
		p.logInfo("v2 pipeline: executing query", "index", i, "purpose", pq.Purpose, "sql", pq.SQL)
	}

	if len(allQueries) == 0 {
		return nil, nil
	}

	// Execute queries in parallel
	results := make([]pipeline.ExecutedQuery, len(allQueries))
	var wg sync.WaitGroup

	for i, pq := range allQueries {
		wg.Add(1)
		go func(idx int, query PlannedQuery) {
			defer wg.Done()

			// Clean up SQL
			sql := strings.TrimSpace(query.SQL)
			sql = strings.TrimSuffix(sql, ";")

			// Execute query
			queryResult, err := p.cfg.Querier.Query(ctx, sql)
			if err != nil {
				// Query execution error (not a SQL error)
				results[idx] = pipeline.ExecutedQuery{
					GeneratedQuery: pipeline.GeneratedQuery{
						DataQuestion: pipeline.DataQuestion{
							Question:  query.Purpose,
							Rationale: query.ExpectedResult,
						},
						SQL:         sql,
						Explanation: query.Purpose,
					},
					Result: pipeline.QueryResult{
						SQL:   sql,
						Error: err.Error(),
					},
				}
				return
			}

			results[idx] = pipeline.ExecutedQuery{
				GeneratedQuery: pipeline.GeneratedQuery{
					DataQuestion: pipeline.DataQuestion{
						Question:  query.Purpose,
						Rationale: query.ExpectedResult,
					},
					SQL:         sql,
					Explanation: query.Purpose,
				},
				Result: queryResult,
			}
		}(i, pq)
	}

	wg.Wait()
	return results, nil
}
