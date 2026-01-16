// Package v2 implements the iterative analysis pipeline.
// The pipeline follows an iterative workflow: interpret → map → plan → execute → inspect → (iterate?) → synthesize
package v2

import (
	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
)

// Interpretation represents the analytical reframing of a user question.
type Interpretation struct {
	// QuestionType categorizes the question (e.g., "count", "comparison", "trend", "lookup")
	QuestionType string `json:"questionType"`
	// Entities are the key entities mentioned (e.g., "validators", "devices", "links")
	Entities []string `json:"entities"`
	// TimeFrame captures any temporal constraints (e.g., "last 7 days", "since epoch 500")
	TimeFrame string `json:"timeFrame,omitempty"`
	// SuccessCriteria describes what a good answer looks like
	SuccessCriteria string `json:"successCriteria"`
	// FailureCriteria describes what would indicate the answer is wrong
	FailureCriteria string `json:"failureCriteria,omitempty"`
	// Reframed is the analytically reframed question
	Reframed string `json:"reframed"`
}

// DataMapping represents the mapping of question concepts to data reality.
type DataMapping struct {
	// Tables are the relevant tables for this question
	Tables []TableMapping `json:"tables"`
	// UnitOfAnalysis is what we're counting/measuring (e.g., "validator", "device-link pair")
	UnitOfAnalysis string `json:"unitOfAnalysis"`
	// Joins describes how tables should be joined
	Joins []JoinSpec `json:"joins,omitempty"`
	// Caveats are data quality or interpretation caveats
	Caveats []string `json:"caveats,omitempty"`
	// Ambiguities are unresolved ambiguities that may affect the answer
	Ambiguities []string `json:"ambiguities,omitempty"`
}

// TableMapping maps a table to its role in answering the question.
type TableMapping struct {
	// Table is the table name
	Table string `json:"table"`
	// Role describes what this table provides (e.g., "validator identity", "connection status")
	Role string `json:"role"`
	// KeyColumns are the important columns from this table
	KeyColumns []string `json:"keyColumns"`
}

// JoinSpec describes how two tables should be joined.
type JoinSpec struct {
	// LeftTable is the left side of the join
	LeftTable string `json:"leftTable"`
	// RightTable is the right side of the join
	RightTable string `json:"rightTable"`
	// JoinType is the type of join (INNER, LEFT, etc.)
	JoinType string `json:"joinType"`
	// Condition is the join condition
	Condition string `json:"condition"`
}

// QueryPlan represents the planned queries to execute.
type QueryPlan struct {
	// ValidationQueries are queries to validate assumptions before answering
	ValidationQueries []PlannedQuery `json:"validationQueries,omitempty"`
	// AnswerQueries are queries that will produce the answer
	AnswerQueries []PlannedQuery `json:"answerQueries"`
}

// PlannedQuery represents a single planned query.
type PlannedQuery struct {
	// Purpose describes what this query is for
	Purpose string `json:"purpose"`
	// SQL is the SQL query
	SQL string `json:"sql"`
	// ExpectedResult describes what we expect to see (for validation)
	ExpectedResult string `json:"expectedResult,omitempty"`
}

// InspectionResult represents the analysis of query results.
type InspectionResult struct {
	// DataQualityOK indicates if the data quality is acceptable
	DataQualityOK bool `json:"dataQualityOk"`
	// Issues are problems found with the results
	Issues []Issue `json:"issues,omitempty"`
	// Learnings are insights gained from the results
	Learnings []string `json:"learnings,omitempty"`
	// ShouldIterate indicates if we should try again with different queries
	ShouldIterate bool `json:"shouldIterate"`
	// Suggestions are suggestions for the next iteration
	Suggestions []string `json:"suggestions,omitempty"`
	// Confidence is the confidence level in the results (0-1)
	Confidence float64 `json:"confidence"`
}

// Issue represents a problem found during inspection.
type Issue struct {
	// Severity is the severity level (error, warning, info)
	Severity string `json:"severity"`
	// Description describes the issue
	Description string `json:"description"`
	// Query is the query that produced the issue (if applicable)
	Query string `json:"query,omitempty"`
}

// IterationState tracks state across iterations of the pipeline.
type IterationState struct {
	// Iteration is the current iteration number (1-indexed)
	Iteration int `json:"iteration"`
	// MaxIterations is the maximum number of iterations allowed
	MaxIterations int `json:"maxIterations"`
	// History contains the history of previous iterations
	History []IterationHistory `json:"history,omitempty"`
}

// IterationHistory records what happened in a previous iteration.
type IterationHistory struct {
	// Iteration is the iteration number
	Iteration int `json:"iteration"`
	// Plan was the query plan for this iteration
	Plan QueryPlan `json:"plan"`
	// Results were the query results
	Results []pipeline.ExecutedQuery `json:"results"`
	// Inspection was the inspection result
	Inspection InspectionResult `json:"inspection"`
}

// V2Result extends PipelineResult with v2-specific data.
type V2Result struct {
	// Embed the base result
	*pipeline.PipelineResult

	// V2-specific fields
	Interpretation *Interpretation   `json:"interpretation,omitempty"`
	DataMapping    *DataMapping      `json:"dataMapping,omitempty"`
	QueryPlan      *QueryPlan        `json:"queryPlan,omitempty"`
	Inspection     *InspectionResult `json:"inspection,omitempty"`
	Iterations     int               `json:"iterations"`
}

// V2Stage represents the current stage of the v2 pipeline.
type V2Stage string

const (
	StageInterpreting V2Stage = "interpreting"
	StageMapping      V2Stage = "mapping"
	StagePlanning     V2Stage = "planning"
	StageExecuting    V2Stage = "executing"
	StageInspecting   V2Stage = "inspecting"
	StageSynthesizing V2Stage = "synthesizing"
	StageComplete     V2Stage = "complete"
	StageError        V2Stage = "error"
)
