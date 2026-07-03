package shreds

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewHTTPClient_HasBoundedTimeout(t *testing.T) {
	c := newHTTPClient(defaultRequestTimeout, defaultMaxConns)
	if c.Timeout == 0 {
		t.Fatal("http client must have a bounded timeout, not http.DefaultClient's infinite one")
	}
	if c.Timeout != defaultRequestTimeout {
		t.Fatalf("expected timeout %s, got %s", defaultRequestTimeout, c.Timeout)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", c.Transport)
	}
	if tr.MaxConnsPerHost != defaultMaxConns {
		t.Fatalf("expected MaxConnsPerHost %d, got %d", defaultMaxConns, tr.MaxConnsPerHost)
	}
}

func TestNewHTTPClient_TimesOutSlowRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
	}))
	defer srv.Close()

	c := newHTTPClient(200*time.Millisecond, defaultMaxConns)
	start := time.Now()
	resp, err := c.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected request to time out, but it succeeded")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("request should have timed out near 200ms, took %s (client not bounding requests)", elapsed)
	}
}
