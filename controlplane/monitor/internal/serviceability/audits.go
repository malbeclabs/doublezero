package serviceability

import (
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
)

// checkUnlinkedInterfaces checks if an interface is in an unlinked state but is part of an activated link.
func checkUnlinkedInterfaces(device serviceability.Device, iface serviceability.Interface, links []serviceability.Link) {
	if iface.Status != serviceability.InterfaceStatusUnlinked {
		return
	}
	for _, link := range links {
		if link.Status != serviceability.LinkStatusActivated {
			continue
		}
		if link.SideAPubKey == device.PubKey && link.SideAIfaceName == iface.Name {
			MetricUnlinkedInterfaceErrors.WithLabelValues(
				base58.Encode(device.PubKey[:]),
				device.Code,
				iface.Name,
				base58.Encode(link.PubKey[:]),
			).Inc()
		}
		if link.SideZPubKey == device.PubKey && link.SideZIfaceName == iface.Name {
			MetricUnlinkedInterfaceErrors.WithLabelValues(
				base58.Encode(device.PubKey[:]),
				device.Code,
				iface.Name,
				base58.Encode(link.PubKey[:]),
			).Inc()
		}
	}
}
