package workflow

import (
	"context"
)

// Runner is the interface for pipeline implementations.
// It provides methods for running the pipeline with varying levels of control.
type Runner interface {
	// Run executes the pipeline for a user question.
	Run(ctx context.Context, userQuestion string) (*WorkflowResult, error)

	// RunWithHistory executes the pipeline with conversation context.
	RunWithHistory(ctx context.Context, userQuestion string, history []ConversationMessage) (*WorkflowResult, error)

	// RunWithProgress executes the pipeline with progress callbacks for streaming updates.
	RunWithProgress(ctx context.Context, userQuestion string, history []ConversationMessage, onProgress ProgressCallback) (*WorkflowResult, error)
}
