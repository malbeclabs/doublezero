package allowlist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jonboulle/clockwork"
)

// ListItem is one item of a Cloudflare IP list.
type ListItem struct {
	IP      string `json:"ip"`
	Comment string `json:"comment,omitempty"`
}

// IPListAPI reads and replaces the items of one Cloudflare IP list.
type IPListAPI interface {
	Items(ctx context.Context) ([]ListItem, error)
	// ReplaceItems replaces every item and returns once Cloudflare has
	// finished the operation.
	ReplaceItems(ctx context.Context, items []ListItem) error
}

// CloudflareTarget keeps the Cloudflare IP list (spec section 3) equal to the
// list. Each item's comment names the devices that hold the address.
type CloudflareTarget struct {
	API IPListAPI
}

func (t *CloudflareTarget) Name() string { return "cloudflare_list" }

// cloudflareItems renders the list as IP list items, sorted by address.
func cloudflareItems(list List) []ListItem {
	codes := map[netip.Addr][]string{}
	for _, d := range list.Devices {
		for _, a := range d.Addrs() {
			codes[a] = append(codes[a], d.Code)
		}
	}
	items := make([]ListItem, 0, len(codes))
	for _, a := range list.Addrs() {
		c := codes[a]
		slices.Sort(c)
		items = append(items, ListItem{IP: a.String(), Comment: strings.Join(slices.Compact(c), " ")})
	}
	return items
}

func (t *CloudflareTarget) Plan(ctx context.Context, list List) (Plan, error) {
	current, err := t.API.Items(ctx)
	if err != nil {
		return Plan{}, err
	}
	var prevAddrs []netip.Addr
	var other []string
	// Keyed on the parsed address, so 45.0.0.1/32 and 45.0.0.1 are one item.
	have := make(map[string]string, len(current))
	for _, it := range current {
		a, err := parseListIP(it.IP)
		if err != nil {
			// A range or an IPv6 address: the sync never writes one, so a
			// write removes it.
			have[it.IP] = it.Comment
			other = append(other, it.IP)
			continue
		}
		have[a.String()] = it.Comment
		prevAddrs = append(prevAddrs, a)
	}
	want := cloudflareItems(list)
	changed := len(have) != len(want)
	for _, it := range want {
		if c, ok := have[it.IP]; !ok || c != it.Comment {
			changed = true
		}
	}
	added, removed := diffAddrs(sortedUnique(prevAddrs), list.Addrs())
	return Plan{
		Target:       t.Name(),
		Changed:      changed,
		PrevAddrs:    len(current),
		AddedAddrs:   added,
		RemovedAddrs: removed,
		RemovedOther: other,
		list:         list,
	}, nil
}

func (t *CloudflareTarget) Apply(ctx context.Context, plan Plan) error {
	return t.API.ReplaceItems(ctx, cloudflareItems(plan.list))
}

// parseListIP reads an item's address. Cloudflare returns a /32 as a bare
// address; anything wider is not an address the sync writes.
func parseListIP(s string) (netip.Addr, error) {
	if a, err := netip.ParseAddr(s); err == nil && a.Unmap().Is4() {
		return a.Unmap(), nil
	}
	if p, err := netip.ParsePrefix(s); err == nil && p.Addr().Is4() && p.Bits() == 32 {
		return p.Addr(), nil
	}
	return netip.Addr{}, fmt.Errorf("%s is not a single IPv4 address", s)
}

// CloudflareIPList is the Cloudflare API client for one account's IP list.
// The token needs "Account Filter Lists: Edit", which covers every list in
// the account, so it is mounted only in the sync's pod.
type CloudflareIPList struct {
	Client    *http.Client
	BaseURL   string // https://api.cloudflare.com/client/v4
	Token     string
	AccountID string
	ListID    string
	Clock     clockwork.Clock
	// PollInterval is how often ReplaceItems checks the bulk operation.
	PollInterval time.Duration
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfEnvelope struct {
	Success    bool            `json:"success"`
	Errors     []cfError       `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo struct {
		Cursors struct {
			After string `json:"after"`
		} `json:"cursors"`
	} `json:"result_info"`
}

func (c *CloudflareIPList) Items(ctx context.Context) ([]ListItem, error) {
	var items []ListItem
	cursor := ""
	for {
		q := url.Values{"per_page": {"500"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		env, err := c.call(ctx, http.MethodGet, fmt.Sprintf("/accounts/%s/rules/lists/%s/items?%s", c.AccountID, c.ListID, q.Encode()), nil)
		if err != nil {
			return nil, fmt.Errorf("listing cloudflare list items: %w", err)
		}
		var page []ListItem
		if err := json.Unmarshal(env.Result, &page); err != nil {
			return nil, fmt.Errorf("decoding cloudflare list items: %w", err)
		}
		items = append(items, page...)
		cursor = env.ResultInfo.Cursors.After
		if cursor == "" {
			return items, nil
		}
	}
}

// ReplaceItems uses "Update all list items" (PUT), which Cloudflare runs as
// an asynchronous bulk operation, then polls the operation until it finishes.
func (c *CloudflareIPList) ReplaceItems(ctx context.Context, items []ListItem) error {
	if len(items) == 0 {
		// The sync never writes an empty list; refuse here too, so no other
		// caller can.
		return errors.New("refusing to replace the cloudflare list with no items")
	}
	body, err := json.Marshal(items)
	if err != nil {
		return err
	}
	env, err := c.call(ctx, http.MethodPut, fmt.Sprintf("/accounts/%s/rules/lists/%s/items", c.AccountID, c.ListID), body)
	if err != nil {
		return fmt.Errorf("replacing cloudflare list items: %w", err)
	}
	var op struct {
		OperationID string `json:"operation_id"`
	}
	if err := json.Unmarshal(env.Result, &op); err != nil || op.OperationID == "" {
		return fmt.Errorf("replacing cloudflare list items: no operation id in the response")
	}
	for {
		env, err := c.call(ctx, http.MethodGet, fmt.Sprintf("/accounts/%s/rules/lists/bulk_operations/%s", c.AccountID, op.OperationID), nil)
		if err != nil {
			return fmt.Errorf("polling cloudflare bulk operation %s: %w", op.OperationID, err)
		}
		var st struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if err := json.Unmarshal(env.Result, &st); err != nil {
			return fmt.Errorf("decoding cloudflare bulk operation %s: %w", op.OperationID, err)
		}
		switch st.Status {
		case "completed":
			return nil
		case "failed":
			return fmt.Errorf("cloudflare bulk operation %s failed: %s", op.OperationID, st.Error)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("cloudflare bulk operation %s still %q: %w", op.OperationID, st.Status, ctx.Err())
		case <-c.Clock.After(c.PollInterval):
		}
	}
}

func (c *CloudflareIPList) call(ctx context.Context, method, path string, body []byte) (*cfEnvelope, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	var env cfEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(raw))
	}
	if resp.StatusCode != http.StatusOK || !env.Success {
		return nil, fmt.Errorf("HTTP %d: %v", resp.StatusCode, env.Errors)
	}
	return &env, nil
}
