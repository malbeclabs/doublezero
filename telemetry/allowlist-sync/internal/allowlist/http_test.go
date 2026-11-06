package allowlist

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestKube(t *testing.T, handler http.HandlerFunc) *KubeConfigMap {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	tokenFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("sa-token\n"), 0o600))
	return &KubeConfigMap{client: srv.Client(), baseURL: srv.URL, tokenFile: tokenFile, namespace: "dz-testnet", name: "gnmic-device-addresses"}
}

func TestKubeConfigMap_GetAndUpdate(t *testing.T) {
	const path = "/api/v1/namespaces/dz-testnet/configmaps/gnmic-device-addresses"
	var put []byte
	k := newTestKube(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, path, r.URL.Path)
		assert.Equal(t, "Bearer sa-token", r.Header.Get("Authorization"))
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"gnmic-device-addresses","resourceVersion":"42","labels":{"a":"b"}},"data":{"k":"v"}}`))
		case http.MethodPut:
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			put, _ = io.ReadAll(r.Body)
			_, _ = w.Write(put)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})
	cm, err := k.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"k": "v"}, cm.Data)
	cm.Data["map.json"] = "{}\n"
	require.NoError(t, k.Update(context.Background(), cm))
	assert.JSONEq(t, `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"gnmic-device-addresses","resourceVersion":"42","labels":{"a":"b"}},"data":{"k":"v","map.json":"{}\n"}}`, string(put))
}

func TestKubeConfigMap_Errors(t *testing.T) {
	status := http.StatusNotFound
	k := newTestKube(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"reason":"x"}`))
	})
	_, err := k.Get(context.Background())
	assert.ErrorIs(t, err, ErrConfigMapNotFound)
	assert.EqualError(t, err, "configmap not found: dz-testnet/gnmic-device-addresses; create it once with `kubectl -n dz-testnet create configmap gnmic-device-addresses`")

	status = http.StatusForbidden
	_, err = k.Get(context.Background())
	assert.EqualError(t, err, `getting configmap dz-testnet/gnmic-device-addresses: HTTP 403: {"reason":"x"}`)

	status = http.StatusConflict
	err = k.Update(context.Background(), &ConfigMap{Metadata: json.RawMessage(`{}`)})
	assert.EqualError(t, err, `updating configmap dz-testnet/gnmic-device-addresses: HTTP 409: {"reason":"x"}`)
}

func newTestCloudflare(t *testing.T, handler http.HandlerFunc) *CloudflareIPList {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &CloudflareIPList{
		Client: srv.Client(), BaseURL: srv.URL, Token: "cf-token", AccountID: "acct", ListID: "list",
		Clock: clockwork.NewRealClock(), PollInterval: time.Millisecond,
	}
}

func cfOK(w http.ResponseWriter, result any, after string) {
	body := map[string]any{"success": true, "errors": []any{}, "result": result}
	if after != "" {
		body["result_info"] = map[string]any{"cursors": map[string]string{"after": after}}
	}
	_ = json.NewEncoder(w).Encode(body)
}

func TestCloudflareIPList_ItemsPages(t *testing.T) {
	c := newTestCloudflare(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer cf-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/accounts/acct/rules/lists/list/items", r.URL.Path)
		assert.Equal(t, "500", r.URL.Query().Get("per_page"))
		if r.URL.Query().Get("cursor") == "" {
			cfOK(w, []ListItem{{IP: "45.0.0.1", Comment: "dz1"}}, "page2")
			return
		}
		assert.Equal(t, "page2", r.URL.Query().Get("cursor"))
		cfOK(w, []ListItem{{IP: "45.0.0.2"}}, "")
	})
	items, err := c.Items(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []ListItem{{IP: "45.0.0.1", Comment: "dz1"}, {IP: "45.0.0.2"}}, items)
}

func TestCloudflareIPList_ReplacePollsTheBulkOperation(t *testing.T) {
	var mu sync.Mutex
	var put []byte
	polls := 0
	c := newTestCloudflare(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/accounts/acct/rules/lists/list/items":
			put, _ = io.ReadAll(r.Body)
			cfOK(w, map[string]string{"operation_id": "op1"}, "")
		case r.Method == http.MethodGet && r.URL.Path == "/accounts/acct/rules/lists/bulk_operations/op1":
			polls++
			status := map[int]string{1: "pending", 2: "running"}[polls]
			if status == "" {
				status = "completed"
			}
			cfOK(w, map[string]string{"id": "op1", "status": status}, "")
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	require.NoError(t, c.ReplaceItems(context.Background(), []ListItem{{IP: "45.0.0.1", Comment: "dz1"}}))
	assert.JSONEq(t, `[{"ip":"45.0.0.1","comment":"dz1"}]`, string(put))
	assert.Equal(t, 3, polls)
}

func TestCloudflareIPList_ReplaceErrors(t *testing.T) {
	c := newTestCloudflare(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			cfOK(w, map[string]string{"operation_id": "op1"}, "")
			return
		}
		cfOK(w, map[string]string{"status": "failed", "error": "list item limit exceeded"}, "")
	})
	assert.EqualError(t, c.ReplaceItems(context.Background(), []ListItem{{IP: "45.0.0.1"}}),
		"cloudflare bulk operation op1 failed: list item limit exceeded")
	assert.EqualError(t, c.ReplaceItems(context.Background(), nil), "refusing to replace the cloudflare list with no items")

	denied := newTestCloudflare(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`))
	})
	assert.EqualError(t, denied.ReplaceItems(context.Background(), []ListItem{{IP: "45.0.0.1"}}),
		"replacing cloudflare list items: HTTP 403: [{10000 Authentication error}]")
	_, err := denied.Items(context.Background())
	assert.EqualError(t, err, "listing cloudflare list items: HTTP 403: [{10000 Authentication error}]")
}

func TestCloudflareIPList_PollStopsAtDeadline(t *testing.T) {
	c := newTestCloudflare(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			cfOK(w, map[string]string{"operation_id": "op1"}, "")
			return
		}
		cfOK(w, map[string]string{"status": "running"}, "")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := c.ReplaceItems(ctx, []ListItem{{IP: "45.0.0.1"}})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestParseListIP(t *testing.T) {
	for s, want := range map[string]string{"45.0.0.1": "45.0.0.1", "45.0.0.1/32": "45.0.0.1"} {
		a, err := parseListIP(s)
		require.NoError(t, err)
		assert.Equal(t, want, a.String())
	}
	for _, s := range []string{"45.0.0.0/24", "2001:db8::/64", "2001:db8::1", "nope"} {
		_, err := parseListIP(s)
		assert.Error(t, err, s)
	}
}
