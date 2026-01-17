package workflow

import (
	"context"
)

// Runner is the interface for workflow implementations.
// It provides methods for running the workflow with varying levels of control.
type Runner interface {
	// Run executes the workflow for a user question.
	Run(ctx context.Context, userQuestion string) (*WorkflowResult, error)

	// RunWithHistory executes the workflow with conversation context.
	RunWithHistory(ctx context.Context, userQuestion string, history []ConversationMessage) (*WorkflowResult, error)

	// RunWithProgress executes the workflow with progress callbacks for streaming updates.
	RunWithProgress(ctx context.Context, userQuestion string, history []ConversationMessage, onProgress ProgressCallback) (*WorkflowResult, error)
}
