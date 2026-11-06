package allowlist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
)

// ErrConfigMapNotFound is returned when gnmic's map ConfigMap does not exist.
// The sync's Role can only get and update it by name (Kubernetes cannot limit
// create to one name), so an operator creates it once.
var ErrConfigMapNotFound = errors.New("configmap not found")

// ConfigMap is the part of a Kubernetes ConfigMap the sync reads and writes.
// Metadata is passed back unchanged, so an update keeps its labels and carries
// its resourceVersion, and a concurrent change fails the update.
type ConfigMap struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   json.RawMessage   `json:"metadata"`
	Data       map[string]string `json:"data,omitempty"`
	BinaryData map[string]string `json:"binaryData,omitempty"`
}

// ConfigMapAPI gets and updates one ConfigMap, named when it is built.
type ConfigMapAPI interface {
	Get(ctx context.Context) (*ConfigMap, error)
	Update(ctx context.Context, cm *ConfigMap) error
}

// GnmicMapTarget writes the GnmicAddressMap into one key of gnmic's map
// ConfigMap (spec sections 1 and 9).
type GnmicMapTarget struct {
	API ConfigMapAPI
	Key string

	current *ConfigMap // read by Plan, written by Apply in the same pass
}

func (t *GnmicMapTarget) Name() string { return "gnmic_map" }

// RenderGnmicMap is the exact file content the target writes.
func RenderGnmicMap(m GnmicAddressMap) (string, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

func (t *GnmicMapTarget) Plan(ctx context.Context, list List) (Plan, error) {
	cm, err := t.API.Get(ctx)
	if err != nil {
		return Plan{}, err
	}
	prev := GnmicAddressMap{}
	raw, ok := cm.Data[t.Key]
	if ok && raw != "" {
		if err := json.Unmarshal([]byte(raw), &prev); err != nil {
			return Plan{}, fmt.Errorf("configmap key %q holds no address map, so the sync cannot tell what a write would remove; fix or empty the key: %w", t.Key, err)
		}
	}
	want, err := RenderGnmicMap(list.GnmicMap())
	if err != nil {
		return Plan{}, err
	}

	var prevAddrs []netip.Addr
	var removedDevices []string
	for pk, ips := range prev {
		if _, ok := list.Devices[pk]; !ok {
			removedDevices = append(removedDevices, pk)
		}
		for _, ip := range ips {
			a, err := netip.ParseAddr(ip)
			if err != nil {
				return Plan{}, fmt.Errorf("configmap key %q: device %s: %q is not an address: %w", t.Key, pk, ip, err)
			}
			prevAddrs = append(prevAddrs, a)
		}
	}
	slices.Sort(removedDevices)
	prevAddrs = sortedUnique(prevAddrs)
	added, removed := diffAddrs(prevAddrs, list.Addrs())

	t.current = cm
	return Plan{
		Target:         t.Name(),
		Changed:        raw != want,
		TracksDevices:  true,
		PrevDevices:    len(prev),
		RemovedDevices: removedDevices,
		PrevAddrs:      len(prevAddrs),
		AddedAddrs:     added,
		RemovedAddrs:   removed,
		list:           list,
	}, nil
}

func (t *GnmicMapTarget) Apply(ctx context.Context, plan Plan) error {
	if t.current == nil {
		return errors.New("apply without a plan")
	}
	want, err := RenderGnmicMap(plan.list.GnmicMap())
	if err != nil {
		return err
	}
	cm := *t.current
	cm.Data = make(map[string]string, len(t.current.Data)+1)
	for k, v := range t.current.Data {
		cm.Data[k] = v
	}
	cm.Data[t.Key] = want
	t.current = nil
	return t.API.Update(ctx, &cm)
}
