package collector

import (
	"context"
	"net/http"

	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
)

// HTTPClient defines the interface for HTTP operations
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// ServiceabilityClient defines the interface for serviceability operations
type ServiceabilityClient interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}
