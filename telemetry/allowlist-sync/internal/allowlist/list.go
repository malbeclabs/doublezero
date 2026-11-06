package allowlist

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
)

// List is the device address list (spec section 2, step 4): every live device
// that is not excluded, with its onchain addresses and its inclusion.
type List struct {
	Devices map[string]Device // keyed by base58 device pubkey
}

type Device struct {
	Code string
	// Onchain is public_ip, every interface address and agent_ip, keeping only
	// global IPv4 addresses. Sorted, no duplicates.
	Onchain []netip.Addr
	// Included is the device's inclusion address when it is not already onchain.
	Included []netip.Addr
}

// Addrs is everything listed for the device. Sorted, no duplicates.
func (d Device) Addrs() []netip.Addr {
	return sortedUnique(append(slices.Clone(d.Onchain), d.Included...))
}

// Addrs is every address on the list. Sorted, no duplicates.
func (l List) Addrs() []netip.Addr {
	var all []netip.Addr
	for _, d := range l.Devices {
		all = append(all, d.Addrs()...)
	}
	return sortedUnique(all)
}

// GnmicMap renders the list as the map gnmic checks registrations against.
func (l List) GnmicMap() GnmicAddressMap {
	m := make(GnmicAddressMap, len(l.Devices))
	for pk, d := range l.Devices {
		addrs := d.Addrs()
		ips := make([]string, len(addrs))
		for i, a := range addrs {
			ips[i] = a.String()
		}
		m[pk] = ips
	}
	return m
}

// Digest identifies the list's exact contents. A refused pass logs it, and the
// operator passes it back to allow that one list (see Config.AllowShrinkTo).
func (l List) Digest() string {
	b, err := json.Marshal(l.GnmicMap()) // map keys marshal sorted
	if err != nil {
		panic(fmt.Sprintf("marshaling address map: %v", err)) // a map of strings always marshals
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// GnmicAddressMap is the file the sync writes into gnmic's ConfigMap: each
// device pubkey (base58) to the IPv4 addresses gnmic accepts a registration
// for that pubkey from, sorted. For example:
//
//	{
//	  "7xK...": ["137.239.195.54", "137.239.195.130"]
//	}
//
// A pubkey with no entry is refused. This is the one place the format is
// defined; change it here if the gnmic side settles on another shape.
type GnmicAddressMap map[string][]string

// ExceptionReview is an exception entry a person should edit: one that no
// longer does anything and can be removed, or one whose code no longer
// matches the ledger. Entries apply by pubkey, so a code mismatch still
// applies: a contributor renaming its device must not stop the sync.
type ExceptionReview struct {
	Kind    string // "inclusion" or "exclusion"
	Pubkey  string
	Code    string // as the exception file has it
	Address string // inclusions only
	// "device_not_on_ledger", "device_not_live", "address_onchain" or
	// "code_mismatch"
	Reason string
}

// Built is one pass's view of the ledger and exception file.
type Built struct {
	List List
	// Codes holds the code of every device on the ledger, live or not.
	Codes map[string]string
	// Excluded holds the pubkeys the exception file excludes.
	Excluded map[string]bool
	Review   []ExceptionReview
}

// isLive matches Device::allow_latency() in the serviceability program: the
// statuses whose devices still pull their config.
func isLive(s serviceability.DeviceStatus) bool {
	switch s {
	case serviceability.DeviceStatusActivated,
		serviceability.DeviceStatusDeviceProvisioning,
		serviceability.DeviceStatusLinkProvisioning,
		serviceability.DeviceStatusDrained:
		return true
	}
	return false
}

// ErrIncompleteLedger marks a ledger read the sync must not act on.
var ErrIncompleteLedger = errors.New("incomplete ledger read")

// checkComplete refuses program data that cannot be a whole ledger: no global
// state, no devices, or a device the SDK flagged with DeserializeError (today
// only an interface count mismatch). The SDK reads a truncated account as
// zeros without an error, so the removal limits are what stop that.
func checkComplete(pd *serviceability.ProgramData) error {
	if pd == nil || pd.GlobalState == nil {
		return fmt.Errorf("%w: no global state account", ErrIncompleteLedger)
	}
	if len(pd.Devices) == 0 {
		return fmt.Errorf("%w: no device accounts", ErrIncompleteLedger)
	}
	for _, d := range pd.Devices {
		if d.DeserializeError != nil {
			return fmt.Errorf("%w: device %s (%s) did not deserialize: %v",
				ErrIncompleteLedger, base58.Encode(d.PubKey[:]), d.Code, d.DeserializeError)
		}
	}
	return nil
}

// Build computes the list from the ledger's devices and the exception file.
func Build(devices []serviceability.Device, exc Exceptions) Built {
	b := Built{
		List:     List{Devices: map[string]Device{}},
		Codes:    make(map[string]string, len(devices)),
		Excluded: make(map[string]bool, len(exc.Exclusions)),
	}
	live := map[string]bool{}
	for _, ex := range exc.Exclusions {
		b.Excluded[ex.Pubkey] = true
	}

	for _, d := range devices {
		pk := base58.Encode(d.PubKey[:])
		b.Codes[pk] = d.Code
		if !isLive(d.Status) {
			continue
		}
		live[pk] = true
		if b.Excluded[pk] {
			continue
		}
		b.List.Devices[pk] = Device{Code: d.Code, Onchain: onchainAddrs(d)}
	}

	for _, ex := range exc.Exclusions {
		r := ExceptionReview{Kind: "exclusion", Pubkey: ex.Pubkey, Code: ex.Code}
		code, onLedger := b.Codes[ex.Pubkey]
		switch {
		case !onLedger:
			r.Reason = "device_not_on_ledger"
		case !live[ex.Pubkey]:
			r.Reason = "device_not_live"
		case code != ex.Code:
			r.Reason = "code_mismatch"
		default:
			continue
		}
		b.Review = append(b.Review, r)
	}

	for _, in := range exc.Inclusions {
		r := ExceptionReview{Kind: "inclusion", Pubkey: in.Pubkey, Code: in.Code, Address: in.Address.String()}
		code, onLedger := b.Codes[in.Pubkey]
		dev, listed := b.List.Devices[in.Pubkey]
		switch {
		case !onLedger:
			r.Reason = "device_not_on_ledger"
		case !listed:
			r.Reason = "device_not_live"
		case slices.Contains(dev.Onchain, in.Address):
			r.Reason = "address_onchain"
		default:
			dev.Included = []netip.Addr{in.Address}
			b.List.Devices[in.Pubkey] = dev
			if code == in.Code {
				continue
			}
			r.Reason = "code_mismatch"
		}
		b.Review = append(b.Review, r)
	}
	return b
}

// onchainAddrs is a device's onchain addresses (spec section 2): public_ip,
// the address of every interface of any type or status, and agent_ip, keeping
// only global IPv4 addresses.
func onchainAddrs(d serviceability.Device) []netip.Addr {
	var out []netip.Addr
	add := func(a netip.Addr) {
		if isGlobalIPv4(a) {
			out = append(out, a)
		}
	}
	add(netip.AddrFrom4(d.PublicIp))
	for _, iface := range d.Interfaces {
		// IpNet is address then prefix length; only the address counts.
		add(netip.AddrFrom4([4]byte(iface.IpNet[:4])))
	}
	// agent_ip: 0.0.0.0 means unset, which is not global and so never listed.
	add(netip.AddrFrom4(d.AgentIp))
	return sortedUnique(out)
}

func sortedUnique(addrs []netip.Addr) []netip.Addr {
	slices.SortFunc(addrs, func(a, b netip.Addr) int { return a.Compare(b) })
	return slices.Compact(addrs)
}
