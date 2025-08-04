package collector

import (
	"context"
	"net/http"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

// HTTPClient defines the interface for HTTP operations
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// ServiceabilityClient defines the interface for serviceability operations
type ServiceabilityClient interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}
