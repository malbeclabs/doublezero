package serviceability

import (
	"log/slog"
	"strconv"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
)

const (
	bgpCommunityMinValid = 10000
	bgpCommunityMaxValid = 10999
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

// checkMetroBGPCommunityRange checks if metro BGP community values are within the valid range (10000-10999).
func checkMetroBGPCommunityRange(log *slog.Logger, metro serviceability.Metro) {
	if metro.BgpCommunity < bgpCommunityMinValid || metro.BgpCommunity > bgpCommunityMaxValid {
		pubkey := base58.Encode(metro.PubKey[:])
		bgpCommunity := strconv.FormatUint(uint64(metro.BgpCommunity), 10)

		MetricMetroBGPCommunityOutOfRange.WithLabelValues(
			pubkey,
			metro.Code,
			bgpCommunity,
		).Inc()

		log.Warn("metro BGP community out of range",
			"metro_pubkey", pubkey,
			"metro_code", metro.Code,
			"bgp_community", metro.BgpCommunity,
			"valid_range", "10000-10999",
		)
	}
}

// checkMetroBGPCommunityDuplicates checks for duplicate BGP community values across metros.
func checkMetroBGPCommunityDuplicates(log *slog.Logger, metros []serviceability.Metro) {
	bgpCommunityMap := make(map[uint16][]serviceability.Metro)

	for _, metro := range metros {
		bgpCommunityMap[metro.BgpCommunity] = append(bgpCommunityMap[metro.BgpCommunity], metro)
	}

	for bgpCommunity, metroList := range bgpCommunityMap {
		if len(metroList) > 1 {
			bgpCommunityStr := strconv.FormatUint(uint64(bgpCommunity), 10)

			for _, metro := range metroList {
				pubkey := base58.Encode(metro.PubKey[:])

				MetricMetroBGPCommunityDuplicates.WithLabelValues(
					pubkey,
					metro.Code,
					bgpCommunityStr,
				).Inc()
			}

			metroCodes := make([]string, len(metroList))
			for i, m := range metroList {
				metroCodes[i] = m.Code
			}

			log.Warn("duplicate BGP community detected",
				"bgp_community", bgpCommunity,
				"metro_count", len(metroList),
				"metro_codes", metroCodes,
			)
		}
	}
}
