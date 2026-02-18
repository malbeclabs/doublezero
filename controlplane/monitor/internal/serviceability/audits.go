package serviceability

import (
	"log/slog"
	"strconv"

	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
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

// checkExchangeBGPCommunityRange checks if exchange BGP community values are within the valid range (10000-10999).
func checkExchangeBGPCommunityRange(log *slog.Logger, exchange serviceability.Exchange) {
	if exchange.BgpCommunity < bgpCommunityMinValid || exchange.BgpCommunity > bgpCommunityMaxValid {
		pubkey := base58.Encode(exchange.PubKey[:])
		bgpCommunity := strconv.FormatUint(uint64(exchange.BgpCommunity), 10)

		MetricExchangeBGPCommunityOutOfRange.WithLabelValues(
			pubkey,
			exchange.Code,
			bgpCommunity,
		).Inc()

		log.Warn("exchange BGP community out of range",
			"exchange_pubkey", pubkey,
			"exchange_code", exchange.Code,
			"bgp_community", exchange.BgpCommunity,
			"valid_range", "10000-10999",
		)
	}
}

// checkExchangeBGPCommunityDuplicates checks for duplicate BGP community values across exchanges.
func checkExchangeBGPCommunityDuplicates(log *slog.Logger, exchanges []serviceability.Exchange) {
	bgpCommunityMap := make(map[uint16][]serviceability.Exchange)

	for _, exchange := range exchanges {
		bgpCommunityMap[exchange.BgpCommunity] = append(bgpCommunityMap[exchange.BgpCommunity], exchange)
	}

	for bgpCommunity, exchangeList := range bgpCommunityMap {
		if len(exchangeList) > 1 {
			bgpCommunityStr := strconv.FormatUint(uint64(bgpCommunity), 10)

			for _, exchange := range exchangeList {
				pubkey := base58.Encode(exchange.PubKey[:])

				MetricExchangeBGPCommunityDuplicates.WithLabelValues(
					pubkey,
					exchange.Code,
					bgpCommunityStr,
				).Inc()
			}

			exchangeCodes := make([]string, len(exchangeList))
			for i, ex := range exchangeList {
				exchangeCodes[i] = ex.Code
			}

			log.Warn("duplicate BGP community detected",
				"bgp_community", bgpCommunity,
				"exchange_count", len(exchangeList),
				"exchange_codes", exchangeCodes,
			)
		}
	}
}
