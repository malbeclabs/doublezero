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

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
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
	log          *slog.Logger
	cfg          *Config
	cacheLinks   []serviceability.Link
	cacheDevices []serviceability.Device
	cacheUsers   []serviceability.User
}

func NewServiceabilityWatcher(cfg *Config) (*ServiceabilityWatcher, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &ServiceabilityWatcher{
		log: cfg.Logger.With("watcher", watcherName),
		cfg: cfg,
	}, nil
}

func (w *ServiceabilityWatcher) Name() string {
	return watcherName
}

func (w *ServiceabilityWatcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

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

		// filter out events for our self-testing
		userEvents = slices.DeleteFunc(userEvents, func(e ServiceabilityUserEvent) bool {
			return base58.Encode(e.User.Owner[:]) == doubleZeroPubKey
		})

		userAdds := 0
		for _, e := range userEvents {
			logEvent(e)
			if e.Type() == EventTypeAdded {
				userAdds++
			}
		}
		if userAdds > 0 && w.cfg.SlackWebhookURL != "" {
			msg, err := w.buildSlackMessage(userEvents, data.Devices)
			if err != nil {
				w.log.Error("failed to build slack message", "error", err)
			}
			if err := w.postSlackMessage(msg); err != nil {
				w.log.Error("failed to post slack message", "error", err)
			}
		}
	}

	// save current on-chain state for next comparison interval
	w.cacheLinks = data.Links
	w.cacheDevices = data.Devices
	w.cacheUsers = data.Users

	return nil
}

func programVersionString(version serviceability.ProgramVersion) string {
	return fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch)
}

func (w *ServiceabilityWatcher) buildSlackMessage(event []ServiceabilityUserEvent, devices []serviceability.Device) (string, error) {
	findDeviceCode := func(pubkey [32]byte) string {
		for _, d := range devices {
			if d.PubKey == pubkey {
				return d.Code
			}
		}
		return "unknown"
	}

	users := [][]string{}
	for _, e := range event {
		if e.Type() == EventTypeAdded {
			userPubKey := base58.Encode(e.User.Owner[:])
			clientIp := net.IP(e.User.ClientIp[:]).String()
			devicePubKey := base58.Encode(e.User.DevicePubKey[:])
			tunnelId := strconv.FormatUint(uint64(e.User.TunnelId), 10)
			users = append(users, []string{
				userPubKey,
				clientIp,
				devicePubKey,
				findDeviceCode(e.User.DevicePubKey),
				tunnelId,
			})
		}
	}
	if len(users) == 0 {
		return "", nil
	}

	title := "New DoubleZero Users Added!"
	if len(users) == 1 {
		title = "New DoubleZero User Added!"
	}

	users = slices.Insert(users, 0, []string{"UserPubKey", "Client IP", "Device PubKey", "Device Name", "Tunnel ID"})
	header := fmt.Sprintf(":yay-frog: :frog-wow-scroll: :elmo-fire: :lfg-dz: %s :lfg-dz: :elmo-fire: :frog-wow-scroll: :yay-frog:", title)
	return GenerateSlackTableMessage(header, users, nil)
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
