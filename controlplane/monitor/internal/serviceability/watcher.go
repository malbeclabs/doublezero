package serviceability

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58"
)

const (
	watcherName = "serviceability"
)

var (
	// DoubleZero pubkey used for self-testing
	doubleZeroPubKey = "DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan"
)

type ServiceabilityWatcher struct {
	log             *slog.Logger
	cfg             *Config
	cacheLinks      []serviceability.Link
	cacheDevices    []serviceability.Device
	cacheUsers      []serviceability.User
	rpcClient       *http.Client
	currDZEpoch     uint64
	currSolanaEpoch uint64
}

func NewServiceabilityWatcher(cfg *Config) (*ServiceabilityWatcher, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &ServiceabilityWatcher{
		log: cfg.Logger.With("watcher", watcherName),
		cfg: cfg,
		rpcClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

func (w *ServiceabilityWatcher) Name() string {
	return watcherName
}

func (w *ServiceabilityWatcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	// if influx writer is configured, monitor errors messages for async writes
	if w.cfg.InfluxWriter != nil {
		go func() {
			for err := range w.cfg.InfluxWriter.Errors() {
				w.log.Error("influx write error", "error", err)
			}
		}()
	}

	err := w.Tick(ctx)
	if err != nil {
		w.log.Error("failed to tick", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			w.log.Debug("context done, stopping")
			return nil
		case <-ticker.C:
			err := w.Tick(ctx)
			if err != nil {
				w.log.Error("failed to tick", "error", err)
			}
		}
	}
}

func (w *ServiceabilityWatcher) Tick(ctx context.Context) error {
	data, err := w.cfg.Serviceability.GetProgramData(ctx)
	if err != nil {
		MetricErrors.WithLabelValues(MetricErrorTypeGetProgramData).Inc()
		return err
	}

	version := programVersionString(data.ProgramConfig.Version)
	MetricProgramBuildInfo.WithLabelValues(version).Set(1)

	w.log.Debug("serviceability data", "program_version", version)

	// we need to null user and reference count fields, else the logs will be noisy.
	for i := range data.Devices {
		data.Devices[i].UsersCount = 0
		data.Devices[i].ReferenceCount = 0
	}
	// filter out our own users
	data.Users = slices.DeleteFunc(data.Users, func(u serviceability.User) bool {
		return base58.Encode(u.Owner[:]) == doubleZeroPubKey
	})

	w.processEvents(data)
	w.runAudits(data)

	if w.cfg.InfluxWriter != nil {
		w.exportDevicesToInflux(data.Devices)
		w.exportContributorsToInflux(data.Contributors)
		w.exportExchangesToInflux(data.Exchanges)
		w.exportLinksToInflux(data.Links)
	}

	// Export multicast publisher block metrics
	w.exportMulticastPublisherBlockMetrics(ctx)

	// save current on-chain state for next comparison interval
	w.cacheLinks = data.Links
	w.cacheDevices = data.Devices
	w.cacheUsers = data.Users

	// detect current epoch info
	w.detectEpochChange("doublezero", w.cfg.LedgerRPCClient, &w.currDZEpoch)
	w.detectEpochChange("solana", w.cfg.SolanaRPCClient, &w.currSolanaEpoch)
	return nil
}

func (w *ServiceabilityWatcher) detectEpochChange(chainName string, rpcClient LedgerRPCClient, lastEpoch *uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	epochInfo, err := rpcClient.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		w.log.Error("failed to get epoch info", "error", err)
		return
	}

	currEpoch := epochInfo.Epoch
	prevEpochStart, nextEpochStart := CalculateEpochTimes(epochInfo.SlotIndex, epochInfo.SlotsInEpoch)
	w.log.Debug("epoch status", "chain", chainName, "current_epoch", currEpoch, "previous_epoch_start", prevEpochStart, "next_epoch_start", nextEpochStart)

	// if epoch is 0, we just restarted
	if currEpoch > *lastEpoch && *lastEpoch != 0 {
		w.log.Info("epoch change detected", "chain", chainName, "prev_epoch_start", prevEpochStart, "next_epoch_start", nextEpochStart, "previous_epoch", *lastEpoch, "current_epoch", currEpoch)

		// send Slack notification for testnet and mainnet-beta only
		if w.cfg.SlackWebhookURL != "" && (w.cfg.Env == config.EnvTestnet || w.cfg.Env == config.EnvMainnetBeta) {
			msg, err := w.buildEpochChangeSlackMessage(w.cfg.Env, chainName, *lastEpoch, currEpoch)
			if err != nil {
				w.log.Error("failed to build epoch change slack message", "error", err)
			} else {
				w.log.Info("posting epoch change slack message", "chain", chainName, "epoch", currEpoch)
				if err := w.postSlackMessage(msg); err != nil {
					w.log.Error("failed to post epoch change slack message", "error", err)
				}
			}
		}
	}
	*lastEpoch = currEpoch
}

func (w *ServiceabilityWatcher) exportDevicesToInflux(devices []serviceability.Device) {
	if w.cfg.InfluxWriter == nil {
		return
	}
	additionalTags := map[string]string{
		"env": w.cfg.Env,
	}
	// write each device as a separate line protocol entry
	for _, device := range devices {
		line, err := ToLineProtocol("devices", device, time.Now(), additionalTags)
		if err != nil {
			w.log.Error("failed to create influx line protocol for device", "device_code", device.Code, "error", err)
			continue
		}
		w.log.Debug("writing device record to influx", "line", line)
		w.cfg.InfluxWriter.WriteRecord(line)
	}
	w.cfg.InfluxWriter.Flush()
}

func (w *ServiceabilityWatcher) exportLinksToInflux(links []serviceability.Link) {
	if w.cfg.InfluxWriter == nil {
		return
	}
	additionalTags := map[string]string{
		"env": w.cfg.Env,
	}
	// write each link as a separate line protocol entry
	for _, link := range links {
		line, err := ToLineProtocol("links", link, time.Now(), additionalTags)
		if err != nil {
			w.log.Error("failed to create influx line protocol for link", "link_code", link.Code, "error", err)
			continue
		}
		w.log.Debug("writing link record to influx", "line", line)
		w.cfg.InfluxWriter.WriteRecord(line)
	}
	w.cfg.InfluxWriter.Flush()
}

func (w *ServiceabilityWatcher) exportContributorsToInflux(contributors []serviceability.Contributor) {
	if w.cfg.InfluxWriter == nil {
		return
	}
	additionalTags := map[string]string{
		"env": w.cfg.Env,
	}
	// write each contributor as a separate line protocol entry
	for _, contributor := range contributors {
		line, err := ToLineProtocol("contributors", contributor, time.Now(), additionalTags)
		if err != nil {
			w.log.Error("failed to create influx line protocol for contributor", "contributor_code", contributor.Code, "error", err)
			continue
		}
		w.log.Debug("writing contributor record to influx", "line", line)
		w.cfg.InfluxWriter.WriteRecord(line)
	}
	w.cfg.InfluxWriter.Flush()
}

func (w *ServiceabilityWatcher) exportExchangesToInflux(exchanges []serviceability.Exchange) {
	if w.cfg.InfluxWriter == nil {
		return
	}
	additionalTags := map[string]string{
		"env": w.cfg.Env,
	}
	// write each exchange as a separate line protocol entry
	for _, exchange := range exchanges {
		line, err := ToLineProtocol("exchanges", exchange, time.Now(), additionalTags)
		if err != nil {
			w.log.Error("failed to create influx line protocol for exchange", "exchange_code", exchange.Code, "error", err)
			continue
		}
		w.log.Debug("writing exchange record to influx", "line", line)
		w.cfg.InfluxWriter.WriteRecord(line)
	}
	w.cfg.InfluxWriter.Flush()
}

func (w *ServiceabilityWatcher) processEvents(data *serviceability.ProgramData) {
	logEvent := func(events ServiceabilityEventer) {
		w.log.Info(
			"serviceability event",
			"entity_type", events.EntityType().String(),
			"action", events.Type().String(),
			"id", events.Id(),
			"pub_key", events.PubKey(),
			"diff", events.Diff())
	}

	if w.cacheDevices != nil {
		deviceEvents := CompareDevice(w.cacheDevices, data.Devices)
		w.log.Debug("device events", "count", len(deviceEvents))
		for _, e := range deviceEvents {
			logEvent(e)
		}
	}

	if w.cacheLinks != nil {
		linkEvents := CompareLink(w.cacheLinks, data.Links)
		w.log.Debug("link events", "count", len(linkEvents))
		for _, e := range linkEvents {
			logEvent(e)
		}
	}

	if w.cacheUsers != nil {
		userEvents := CompareUser(w.cacheUsers, data.Users)
		w.log.Debug("user events", "count", len(userEvents))

		var newUsers []ServiceabilityUserEvent
		for _, e := range userEvents {
			logEvent(e)
			if e.Type() == EventTypeAdded {
				newUsers = append(newUsers, e)
			}
		}

		if len(newUsers) > 0 && w.cfg.SlackWebhookURL != "" {
			w.log.Info("notifying new users", "count", len(newUsers))
			w.notifyNewUsers(newUsers, data.Devices, len(data.Users))
		}
	}
}

func (w *ServiceabilityWatcher) runAudits(data *serviceability.ProgramData) {
	if w.cacheDevices == nil && w.cacheLinks == nil {
		return
	}
	for _, device := range data.Devices {
		for _, iface := range device.Interfaces {
			checkUnlinkedInterfaces(device, iface, data.Links)
		}
	}

	for _, exchange := range data.Exchanges {
		checkExchangeBGPCommunityRange(w.log, exchange)
	}

	checkExchangeBGPCommunityDuplicates(w.log, data.Exchanges)
}

func programVersionString(version serviceability.ProgramVersion) string {
	return fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch)
}

func (w *ServiceabilityWatcher) buildSlackMessage(event []ServiceabilityUserEvent, devices []serviceability.Device, totalUsers int) (string, error) {
	if len(event) == 0 {
		return "", nil
	}

	findDeviceCode := func(pubkey [32]byte) string {
		for _, d := range devices {
			if d.PubKey == pubkey {
				return d.Code
			}
		}
		return "not found"
	}

	users := [][]string{}
	for _, e := range event {
		users = append(users, []string{
			base58.Encode(e.User.Owner[:]),
			net.IP(e.User.ClientIp[:]).String(),
			base58.Encode(e.User.DevicePubKey[:]),
			findDeviceCode(e.User.DevicePubKey),
			strconv.FormatUint(uint64(e.User.TunnelId), 10),
		})
	}

	title := "New DoubleZero Users Added!"
	if len(users) == 1 {
		title = "New DoubleZero User Added!"
	}

	users = slices.Insert(users, 0, []string{"UserPubKey", "Client IP", "Device PubKey", "Device Name", "Tunnel ID"})
	header := fmt.Sprintf(":yay-frog: :frog-wow-scroll: :elmo-fire: :lfg-dz: %s :lfg-dz: :elmo-fire: :frog-wow-scroll: :yay-frog:", title)
	footer := fmt.Sprintf("Total Users: %d", totalUsers)
	return GenerateSlackTableMessage(header, users, nil, footer)
}

func (w *ServiceabilityWatcher) buildEpochChangeSlackMessage(environment, network string, previousEpoch, currentEpoch uint64) (string, error) {
	timestamp := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")

	rows := [][]string{
		{"Environment", "Network", "Previous Epoch", "Current Epoch", "Timestamp"},
		{environment, network, strconv.FormatUint(previousEpoch, 10), strconv.FormatUint(currentEpoch, 10), timestamp},
	}

	header := "Epoch Change Detected"
	return GenerateSlackTableMessage(header, rows, nil, "")
}

func (w *ServiceabilityWatcher) notifyNewUsers(newUsers []ServiceabilityUserEvent, devices []serviceability.Device, totalUsers int) {
	msg, err := w.buildSlackMessage(newUsers, devices, totalUsers)
	if err != nil {
		w.log.Error("failed to build slack message", "error", err)
	}
	w.log.Info("posting slack message", "message", msg)
	if err := w.postSlackMessage(msg); err != nil {
		w.log.Error("failed to post slack message", "error", err)
	}
}

func (w *ServiceabilityWatcher) postSlackMessage(msg string) error {
	req, err := http.NewRequest("POST", w.cfg.SlackWebhookURL, strings.NewReader(msg))
	if err != nil {
		return fmt.Errorf("error creating HTTP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error sending HTTP request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("non-2xx response from Slack: %d", resp.StatusCode)
	}
	return nil
}

func (w *ServiceabilityWatcher) exportMulticastPublisherBlockMetrics(ctx context.Context) {
	ext, err := w.cfg.Serviceability.GetMulticastPublisherBlockResourceExtension(ctx)
	if err != nil {
		w.log.Error("failed to fetch multicast publisher block resource extension", "error", err)
		return
	}

	if ext == nil {
		// Account not yet initialized, set metrics to zero
		w.log.Debug("multicast publisher block resource extension not yet initialized")
		MetricMulticastPublisherBlockTotalIPs.Set(0)
		MetricMulticastPublisherBlockAllocatedIPs.Set(0)
		MetricMulticastPublisherBlockUtilizationPct.Set(0)
		return
	}

	totalIPs := ext.TotalCapacity()
	allocatedIPs := ext.AllocatedCount()
	utilizationPct := 0.0
	if totalIPs > 0 {
		utilizationPct = (float64(allocatedIPs) / float64(totalIPs)) * 100.0
	}

	w.log.Debug("multicast publisher block metrics",
		"total_ips", totalIPs,
		"allocated_ips", allocatedIPs,
		"utilization_pct", utilizationPct,
		"base_net", ext.BaseNetString())

	MetricMulticastPublisherBlockTotalIPs.Set(float64(totalIPs))
	MetricMulticastPublisherBlockAllocatedIPs.Set(float64(allocatedIPs))
	MetricMulticastPublisherBlockUtilizationPct.Set(utilizationPct)
}
