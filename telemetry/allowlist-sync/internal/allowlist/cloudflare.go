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
	"unicode/utf8"

	"github.com/jonboulle/clockwork"
)

// ListItem is one item of a Cloudflare IP list. ID is set on items read from
// Cloudflare and empty on items the sync adds.
type ListItem struct {
	ID      string `json:"id,omitempty"`
	IP      string `json:"ip"`
	Comment string `json:"comment,omitempty"`
	// CreatedOn is Cloudflare's RFC 3339 creation time, set on items read
	// from Cloudflare. Of two items for one address, the older one stays.
	CreatedOn string `json:"created_on,omitempty"`
}

// maxCommentBytes is Cloudflare's limit on an item's comment (500
// characters). A comment is cut to it in bytes, at a character boundary, so
// an address many devices share cannot fail the write.
const maxCommentBytes = 500

func truncateComment(s string) string {
	if len(s) <= maxCommentBytes {
		return s
	}
	cut := maxCommentBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// IPListAPI reads one Cloudflare IP list and adds or deletes its items. Each
// write returns once Cloudflare has finished the operation.
type IPListAPI interface {
	Items(ctx context.Context) ([]ListItem, error)
	// AddItems adds items for addresses the list does not hold. The sync
	// never sends an address the list already holds, because Cloudflare's API
	// docs do not say whether that updates, duplicates or is refused.
	AddItems(ctx context.Context, items []ListItem) error
	// DeleteItems deletes the items with these ids.
	DeleteItems(ctx context.Context, ids []string) error
}

// CloudflareTarget keeps the Cloudflare IP list (spec section 3) equal to the
// list. Each item's comment names the devices that hold the address.
//
// It writes the difference, never the whole list: it adds the items for
// missing addresses, then deletes by id the items that left the list and all
// but the oldest item of an address held twice. Cloudflare's docs do not say whether "Update all list
// items", which removes every item and then adds the new ones, is atomic for
// the rules that read the list, so an address that stays on the list is never
// taken out of it.
type CloudflareTarget struct {
	API IPListAPI

	seen lastSeen
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
		items = append(items, ListItem{IP: a.String(), Comment: truncateComment(strings.Join(slices.Compact(c), " "))})
	}
	return items
}

// listKey is how two items are compared: a single IPv4 address by its parsed
// form, so 45.0.0.1/32 and 45.0.0.1 are one item, and anything else as
// Cloudflare returned it.
func listKey(ip string) (string, bool) {
	a, err := parseListIP(ip)
	if err != nil {
		return ip, false
	}
	return a.String(), true
}

func (t *CloudflareTarget) Plan(ctx context.Context, list List) (Plan, error) {
	current, err := t.API.Items(ctx)
	if err != nil {
		return Plan{}, err
	}
	want := cloudflareItems(list)
	wanted := make(map[string]bool, len(want))
	for _, it := range want {
		wanted[it.IP] = true
	}

	var prevAddrs []netip.Addr
	var other, deleteIDs []string
	have := make(map[string]string, len(current))
	// kept is the one item per wanted address that stays: the oldest, when
	// the list holds more than one for it.
	kept := make(map[string]ListItem, len(current))
	for _, it := range current {
		key, single := listKey(it.IP)
		have[key] = it.Comment
		if single {
			a, _ := netip.ParseAddr(key)
			prevAddrs = append(prevAddrs, a)
		} else {
			// A range or an IPv6 address: the sync never writes one, so a
			// write removes it.
			other = append(other, it.IP)
		}
		if !wanted[key] {
			deleteIDs = append(deleteIDs, it.ID)
			continue
		}
		k, dup := kept[key]
		switch {
		case !dup:
			kept[key] = it
		case it.CreatedOn != "" && (k.CreatedOn == "" || it.CreatedOn < k.CreatedOn):
			deleteIDs = append(deleteIDs, k.ID)
			kept[key] = it
		default:
			deleteIDs = append(deleteIDs, it.ID)
		}
	}
	// An address already on the list is never sent again, so nothing here
	// depends on what Cloudflare does with an address it holds. A comment that
	// no longer names the address's devices stays as it is: comments are for
	// people reading the dashboard, and no rule reads them.
	after := make(map[string]string, len(want))
	for k, it := range kept {
		after[k] = it.Comment
	}
	var add []ListItem
	for _, it := range want {
		if _, ok := kept[it.IP]; !ok {
			add = append(add, it)
			after[it.IP] = it.Comment
		}
	}

	externalAdded, externalRemoved := t.seen.observe(have)

	added, removed := diffAddrs(sortedUnique(prevAddrs), list.Addrs())
	return Plan{
		Target:          t.Name(),
		Changed:         len(add) > 0 || len(deleteIDs) > 0,
		PrevAddrs:       len(current),
		AddedAddrs:      added,
		RemovedAddrs:    removed,
		RemovedOther:    other,
		ExternalAdded:   externalAdded,
		ExternalRemoved: externalRemoved,
		list:            list,
		cfAdd:           add,
		cfDeleteIDs:     deleteIDs,
		cfAfter:         after,
	}, nil
}

func (t *CloudflareTarget) Apply(ctx context.Context, plan Plan) error {
	if len(plan.list.Addrs()) == 0 {
		// The sync never writes an empty list; refuse here too, so no other
		// caller can.
		return errors.New("refusing to empty the cloudflare list")
	}
	t.seen.writing()
	if len(plan.cfAdd) > 0 {
		if err := t.API.AddItems(ctx, plan.cfAdd); err != nil {
			return err
		}
	}
	if len(plan.cfDeleteIDs) > 0 {
		if err := t.API.DeleteItems(ctx, plan.cfDeleteIDs); err != nil {
			return err
		}
	}
	t.seen.wrote(plan.cfAfter)
	return nil
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
	// PollInterval is how often a write checks its bulk operation.
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

// AddItems uses "Create list items" (POST), which Cloudflare runs as an
// asynchronous bulk operation, then waits for the operation to finish.
func (c *CloudflareIPList) AddItems(ctx context.Context, items []ListItem) error {
	body, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return c.bulk(ctx, http.MethodPost, body, "adding cloudflare list items")
}

// DeleteItems uses "Delete list items" (DELETE) by item id, which Cloudflare
// runs as an asynchronous bulk operation, then waits for it to finish.
func (c *CloudflareIPList) DeleteItems(ctx context.Context, ids []string) error {
	type itemID struct {
		ID string `json:"id"`
	}
	req := struct {
		Items []itemID `json:"items"`
	}{}
	for _, id := range ids {
		if id == "" {
			return errors.New("deleting cloudflare list items: an item has no id")
		}
		req.Items = append(req.Items, itemID{ID: id})
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return c.bulk(ctx, http.MethodDelete, body, "deleting cloudflare list items")
}

// bulk sends one write to the list's items and polls the bulk operation it
// starts until the operation completes or fails. Cloudflare allows one
// pending bulk operation per account, so the caller sends the next write only
// after this one returns.
func (c *CloudflareIPList) bulk(ctx context.Context, method string, body []byte, what string) error {
	env, err := c.call(ctx, method, fmt.Sprintf("/accounts/%s/rules/lists/%s/items", c.AccountID, c.ListID), body)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	var op struct {
		OperationID string `json:"operation_id"`
	}
	if err := json.Unmarshal(env.Result, &op); err != nil || op.OperationID == "" {
		return fmt.Errorf("%s: no operation id in the response", what)
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
