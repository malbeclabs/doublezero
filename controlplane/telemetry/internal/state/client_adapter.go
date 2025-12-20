package state

import (
	"context"
	"time"

	stateingest "github.com/malbeclabs/doublezero/telemetry/state-ingest/pkg/client"
)

// ClientAdapter wraps the state-ingest client to implement the StateIngestClient interface.
type ClientAdapter struct {
	client *stateingest.Client
}

// NewClientAdapter creates a new adapter that wraps the state-ingest client.
func NewClientAdapter(client *stateingest.Client) *ClientAdapter {
	return &ClientAdapter{client: client}
}

// UploadSnapshot implements StateIngestClient interface.
func (a *ClientAdapter) UploadSnapshot(ctx context.Context, kind string, timestamp time.Time, data []byte) (string, error) {
	return a.client.UploadSnapshot(ctx, kind, timestamp, data)
}

// GetStateToCollect implements StateIngestClient interface.
func (a *ClientAdapter) GetStateToCollect(ctx context.Context) ([]ShowCommand, error) {
	resp, err := a.client.GetStateToCollect(ctx)
	if err != nil {
		return nil, err
	}
	// Convert from state-ingest types to collector types
	result := make([]ShowCommand, len(resp.ShowCommands))
	for i, sc := range resp.ShowCommands {
		result[i] = ShowCommand{
			Kind:    sc.Kind,
			Command: sc.Command,
		}
	}
	return result, nil
}
