package pipeline

import (
	"context"
)

// Runner is the common interface for pipeline implementations (v1 and v2).
// It provides methods for running the pipeline with varying levels of control.
type Runner interface {
	// Run executes the pipeline for a user question.
	Run(ctx context.Context, userQuestion string) (*PipelineResult, error)

	// RunWithHistory executes the pipeline with conversation context.
	RunWithHistory(ctx context.Context, userQuestion string, history []ConversationMessage) (*PipelineResult, error)

	// RunWithProgress executes the pipeline with progress callbacks for streaming updates.
	RunWithProgress(ctx context.Context, userQuestion string, history []ConversationMessage, onProgress ProgressCallback) (*PipelineResult, error)
}

// Version represents the pipeline version.
type Version string

const (
	VersionV1 Version = "v1"
	VersionV2 Version = "v2"
	VersionV3 Version = "v3"
)
