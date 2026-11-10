package allowlist

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const serviceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"

// KubeConfigMap gets and updates one ConfigMap through the Kubernetes API with
// the pod's service account. It speaks plain HTTP rather than pulling in
// client-go for two calls.
type KubeConfigMap struct {
	client    *http.Client
	baseURL   string // https://host:port
	tokenFile string
	namespace string
	name      string
}

// NewInClusterConfigMap builds the client from the in-cluster environment:
// KUBERNETES_SERVICE_HOST and _PORT, and the service account's CA and token.
// An empty namespace means the pod's own.
func NewInClusterConfigMap(namespace, name string) (*KubeConfigMap, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, fmt.Errorf("not running in a cluster: KUBERNETES_SERVICE_HOST or KUBERNETES_SERVICE_PORT is unset")
	}
	caPEM, err := os.ReadFile(serviceAccountDir + "/ca.crt")
	if err != nil {
		return nil, fmt.Errorf("reading service account CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("service account CA holds no certificate")
	}
	if namespace == "" {
		ns, err := os.ReadFile(serviceAccountDir + "/namespace")
		if err != nil {
			return nil, fmt.Errorf("reading pod namespace (or set --namespace): %w", err)
		}
		namespace = strings.TrimSpace(string(ns))
	}
	return &KubeConfigMap{
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
		},
		baseURL:   "https://" + net.JoinHostPort(host, port),
		tokenFile: serviceAccountDir + "/token",
		namespace: namespace,
		name:      name,
	}, nil
}

func (k *KubeConfigMap) url() string {
	return fmt.Sprintf("%s/api/v1/namespaces/%s/configmaps/%s", k.baseURL, url.PathEscape(k.namespace), url.PathEscape(k.name))
}

func (k *KubeConfigMap) Get(ctx context.Context) (*ConfigMap, error) {
	body, status, err := k.do(ctx, http.MethodGet, nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s/%s; create it once with `kubectl -n %s create configmap %s`",
			ErrConfigMapNotFound, k.namespace, k.name, k.namespace, k.name)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("getting configmap %s/%s: HTTP %d: %s", k.namespace, k.name, status, truncate(body))
	}
	var cm ConfigMap
	if err := json.Unmarshal(body, &cm); err != nil {
		return nil, fmt.Errorf("decoding configmap %s/%s: %w", k.namespace, k.name, err)
	}
	return &cm, nil
}

func (k *KubeConfigMap) Update(ctx context.Context, cm *ConfigMap) error {
	payload, err := json.Marshal(cm)
	if err != nil {
		return err
	}
	body, status, err := k.do(ctx, http.MethodPut, payload)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("updating configmap %s/%s: HTTP %d: %s", k.namespace, k.name, status, truncate(body))
	}
	return nil
}

func (k *KubeConfigMap) do(ctx context.Context, method string, payload []byte) ([]byte, int, error) {
	// Re-read the token on every call: a projected service account token is
	// rotated on disk while the pod runs.
	token, err := os.ReadFile(k.tokenFile)
	if err != nil {
		return nil, 0, fmt.Errorf("reading service account token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, k.url(), bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := k.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%s configmap %s/%s: %w", method, k.namespace, k.name, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("%s configmap %s/%s: reading response: %w", method, k.namespace, k.name, err)
	}
	return body, resp.StatusCode, nil
}

func truncate(b []byte) string {
	const max = 512
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}
