package rpc

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Rate-limit headroom as reported by the endpoint, by JSON-RPC method. Method
// cardinality is bounded by the Solana JSON-RPC surface, so it is safe as a label.
//
// These exist because a 429 is otherwise opaque. solana-go's error types carry
// only a code — *HTTPError is {Code int, err error} and *RPCError is
// {Code, Message, Data} — and the http.Response is discarded inside CallForInto,
// so by the time a rate limit reaches a caller the headers that say what the cap
// actually was are gone. A caller could see "Too many requests for a specific RPC
// call" for hours without ever learning the number it was exceeding.
//
// Rate limits are enforced per method, per source IP, over a rolling window, so
// `remaining` approaching zero on one method is the signal that matters — a global
// request-rate graph will not show it, and neither will a graph of another method.
var (
	rateLimitMethodLimit = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "doublezero_solana_rpc_ratelimit_method_limit",
		Help: "Endpoint-reported request cap for a JSON-RPC method, per rolling rate-limit window.",
	}, []string{"method"})

	rateLimitMethodRemaining = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "doublezero_solana_rpc_ratelimit_method_remaining",
		Help: "Endpoint-reported requests left for a JSON-RPC method in the current rate-limit window.",
	}, []string{"method"})

	// responsesTotal counts responses by the serving node the endpoint names in
	// X-Ratelimit-Node / X-RPC-Node, or "unknown" when it names none.
	//
	// Rate limits are enforced per source IP, so when a provider asks whether load is
	// concentrated on one of their nodes — and whether spreading source IPs would help
	// — this is the answer. Node identity is not on the error path, so it cannot be
	// recovered after a refusal; recording it per response is what makes it available
	// at all. Cardinality is the endpoint's node count, which is small.
	responsesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_solana_rpc_responses_total",
		Help: "JSON-RPC responses received, by the serving node the endpoint reports.",
	}, []string{"node"})
)

const (
	headerMethodLimit     = "X-Ratelimit-Method-Limit"
	headerMethodRemaining = "X-Ratelimit-Method-Remaining"
	headerRPCNode         = "X-RPC-Node"

	// maxObservedBody bounds how much of a request body is read to recover the
	// method name. JSON-RPC requests put "method" near the front, and this only
	// runs on responses that already carry rate-limit headers.
	maxObservedBody = 512
)

// rateLimitObserver records rate-limit headers into metrics as responses pass
// through. It never alters the request or response and never fails a call: an
// endpoint that reports nothing simply produces no series.
type rateLimitObserver struct{ inner http.RoundTripper }

func (o *rateLimitObserver) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := o.inner.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}

	node := resp.Header.Get(headerRPCNode)
	if node == "" {
		node = "unknown"
	}
	responsesTotal.WithLabelValues(node).Inc()

	// Cheap exit first. Most endpoints report nothing, and this must not add
	// per-request work on the hot path when there is nothing to record — so the
	// request body is only inspected once a header proves it is worth it.
	limit := resp.Header.Get(headerMethodLimit)
	remaining := resp.Header.Get(headerMethodRemaining)
	if limit == "" && remaining == "" {
		return resp, nil
	}

	method := requestMethod(req)
	if method == "" {
		return resp, nil
	}
	if v, convErr := strconv.ParseFloat(limit, 64); convErr == nil {
		rateLimitMethodLimit.WithLabelValues(method).Set(v)
	}
	if v, convErr := strconv.ParseFloat(remaining, 64); convErr == nil {
		rateLimitMethodRemaining.WithLabelValues(method).Set(v)
	}
	return resp, nil
}

// requestMethod recovers the JSON-RPC method from a request body, or "batch" for
// a batch request whose members may differ. It reads through GetBody so the body
// the transport is sending is left untouched; a request without GetBody (not
// produced by the clients in this package) yields "" and is skipped.
func requestMethod(req *http.Request) string {
	if req.GetBody == nil {
		return ""
	}
	body, err := req.GetBody()
	if err != nil {
		return ""
	}
	defer body.Close()

	buf, err := io.ReadAll(io.LimitReader(body, maxObservedBody))
	if err != nil || len(buf) == 0 {
		return ""
	}
	for _, b := range buf {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		case '[':
			return "batch"
		}
		break
	}
	// Decode into just the method field; a truncated body fails cleanly to "".
	var probe struct {
		Method string `json:"method"`
	}
	if json.Unmarshal(buf, &probe) != nil {
		return ""
	}
	return probe.Method
}
