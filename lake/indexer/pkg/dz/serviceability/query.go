package dzsvc

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
)

// deviceRow is an intermediate struct for reading from ClickHouse
// It matches the exact column types in ClickHouse
type deviceRow struct {
	PK            string `ch:"pk"`
	Status        string `ch:"status"`
	DeviceType    string `ch:"device_type"`
	Code          string `ch:"code"`
	PublicIP      string `ch:"public_ip"`
	ContributorPK string `ch:"contributor_pk"`
	MetroPK       string `ch:"metro_pk"`
	MaxUsers      int32  `ch:"max_users"`
}

// userRow is an intermediate struct for reading from ClickHouse
type userRow struct {
	PK          string `ch:"pk"`
	OwnerPubkey string `ch:"owner_pubkey"`
	Status      string `ch:"status"`
	Kind        string `ch:"kind"`
	ClientIP    string `ch:"client_ip"`
	DZIP        string `ch:"dz_ip"`
	DevicePK    string `ch:"device_pk"`
	TunnelID    int32  `ch:"tunnel_id"`
}

// linkRow is an intermediate struct for reading from ClickHouse
type linkRow struct {
	PK                  string `ch:"pk"`
	Status              string `ch:"status"`
	Code                string `ch:"code"`
	TunnelNet           string `ch:"tunnel_net"`
	ContributorPK       string `ch:"contributor_pk"`
	SideAPK             string `ch:"side_a_pk"`
	SideZPK             string `ch:"side_z_pk"`
	SideAIfaceName      string `ch:"side_a_iface_name"`
	SideZIfaceName      string `ch:"side_z_iface_name"`
	LinkType            string `ch:"link_type"`
	CommittedRTTNs      int64  `ch:"committed_rtt_ns"`
	CommittedJitterNs   int64  `ch:"committed_jitter_ns"`
	Bandwidth           int64  `ch:"bandwidth_bps"`
	ISISDelayOverrideNs int64  `ch:"isis_delay_override_ns"`
}

// metroRow is an intermediate struct for reading from ClickHouse
type metroRow struct {
	PK        string  `ch:"pk"`
	Code      string  `ch:"code"`
	Name      string  `ch:"name"`
	Longitude float64 `ch:"longitude"`
	Latitude  float64 `ch:"latitude"`
}

// contributorRow is an intermediate struct for reading from ClickHouse
type contributorRow struct {
	PK   string `ch:"pk"`
	Code string `ch:"code"`
	Name string `ch:"name"`
}

// QueryCurrentContributors queries all current (non-deleted) contributors from ClickHouse
func QueryCurrentContributors(ctx context.Context, log *slog.Logger, db clickhouse.Client) ([]Contributor, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	d, err := NewContributorDataset(log)
	if err != nil {
		return nil, fmt.Errorf("failed to create dataset: %w", err)
	}

	typed := dataset.NewTypedDimensionType2Dataset[contributorRow](d)
	rows, err := typed.GetCurrentRows(ctx, conn, nil) // nil = all entities
	if err != nil {
		return nil, fmt.Errorf("failed to query contributors: %w", err)
	}

	contributors := make([]Contributor, len(rows))
	for i, row := range rows {
		contributors[i] = Contributor{
			PK:   row.PK,
			Code: row.Code,
			Name: row.Name,
		}
	}

	return contributors, nil
}

// QueryCurrentDevices queries all current (non-deleted) devices from ClickHouse
func QueryCurrentDevices(ctx context.Context, log *slog.Logger, db clickhouse.Client) ([]Device, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	d, err := NewDeviceDataset(log)
	if err != nil {
		return nil, fmt.Errorf("failed to create dataset: %w", err)
	}

	typed := dataset.NewTypedDimensionType2Dataset[deviceRow](d)
	rows, err := typed.GetCurrentRows(ctx, conn, nil) // nil = all entities
	if err != nil {
		return nil, fmt.Errorf("failed to query devices: %w", err)
	}

	devices := make([]Device, len(rows))
	for i, row := range rows {
		devices[i] = Device{
			PK:            row.PK,
			Status:        row.Status,
			DeviceType:    row.DeviceType,
			Code:          row.Code,
			PublicIP:      row.PublicIP,
			ContributorPK: row.ContributorPK,
			MetroPK:       row.MetroPK,
			MaxUsers:      uint16(row.MaxUsers),
		}
	}

	return devices, nil
}

// QueryCurrentLinks queries all current (non-deleted) links from ClickHouse
func QueryCurrentLinks(ctx context.Context, log *slog.Logger, db clickhouse.Client) ([]Link, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	d, err := NewLinkDataset(log)
	if err != nil {
		return nil, fmt.Errorf("failed to create dataset: %w", err)
	}

	typed := dataset.NewTypedDimensionType2Dataset[linkRow](d)
	rows, err := typed.GetCurrentRows(ctx, conn, nil) // nil = all entities
	if err != nil {
		return nil, fmt.Errorf("failed to query links: %w", err)
	}

	links := make([]Link, len(rows))
	for i, row := range rows {
		links[i] = Link{
			PK:                  row.PK,
			Status:              row.Status,
			Code:                row.Code,
			TunnelNet:           row.TunnelNet,
			ContributorPK:       row.ContributorPK,
			SideAPK:             row.SideAPK,
			SideZPK:             row.SideZPK,
			SideAIfaceName:      row.SideAIfaceName,
			SideZIfaceName:      row.SideZIfaceName,
			LinkType:            row.LinkType,
			CommittedRTTNs:      uint64(row.CommittedRTTNs),
			CommittedJitterNs:   uint64(row.CommittedJitterNs),
			Bandwidth:           uint64(row.Bandwidth),
			ISISDelayOverrideNs: uint64(row.ISISDelayOverrideNs),
		}
	}

	return links, nil
}

// QueryCurrentMetros queries all current (non-deleted) metros from ClickHouse
func QueryCurrentMetros(ctx context.Context, log *slog.Logger, db clickhouse.Client) ([]Metro, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	d, err := NewMetroDataset(log)
	if err != nil {
		return nil, fmt.Errorf("failed to create dataset: %w", err)
	}

	typed := dataset.NewTypedDimensionType2Dataset[metroRow](d)
	rows, err := typed.GetCurrentRows(ctx, conn, nil) // nil = all entities
	if err != nil {
		return nil, fmt.Errorf("failed to query metros: %w", err)
	}

	metros := make([]Metro, len(rows))
	for i, row := range rows {
		metros[i] = Metro{
			PK:        row.PK,
			Code:      row.Code,
			Name:      row.Name,
			Longitude: row.Longitude,
			Latitude:  row.Latitude,
		}
	}

	return metros, nil
}

// QueryCurrentUsers queries all current (non-deleted) users from ClickHouse
// Uses history table with deterministic "latest row per entity" definition
func QueryCurrentUsers(ctx context.Context, log *slog.Logger, db clickhouse.Client) ([]User, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	d, err := NewUserDataset(log)
	if err != nil {
		return nil, fmt.Errorf("failed to create dataset: %w", err)
	}

	typed := dataset.NewTypedDimensionType2Dataset[userRow](d)
	rows, err := typed.GetCurrentRows(ctx, conn, nil) // nil = all entities
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %w", err)
	}

	users := make([]User, len(rows))
	for i, row := range rows {
		users[i] = User{
			PK:          row.PK,
			OwnerPubkey: row.OwnerPubkey,
			Status:      row.Status,
			Kind:        row.Kind,
			DevicePK:    row.DevicePK,
			TunnelID:    uint16(row.TunnelID),
		}
		if row.ClientIP != "" {
			users[i].ClientIP = net.ParseIP(row.ClientIP)
		}
		if row.DZIP != "" {
			users[i].DZIP = net.ParseIP(row.DZIP)
		}
	}

	return users, nil
}
