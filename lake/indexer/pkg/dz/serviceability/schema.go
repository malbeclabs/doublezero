package dzsvc

import (
	"log/slog"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
)

// ContributorSchema defines the schema for contributors
type ContributorSchema struct{}

func (s *ContributorSchema) Name() string {
	return "dz_contributors"
}

func (s *ContributorSchema) PrimaryKeyColumns() []string {
	return []string{"pk:VARCHAR"}
}

func (s *ContributorSchema) PayloadColumns() []string {
	return []string{"code:VARCHAR", "name:VARCHAR"}
}

func (s *ContributorSchema) ToRow(c Contributor) []any {
	return []any{c.PK, c.Code, c.Name}
}

func (s *ContributorSchema) GetPrimaryKey(c Contributor) string {
	return c.PK
}

// DeviceSchema defines the schema for devices
type DeviceSchema struct{}

func (s *DeviceSchema) Name() string {
	return "dz_devices"
}

func (s *DeviceSchema) PrimaryKeyColumns() []string {
	return []string{"pk:VARCHAR"}
}

func (s *DeviceSchema) PayloadColumns() []string {
	return []string{
		"status:VARCHAR",
		"device_type:VARCHAR",
		"code:VARCHAR",
		"public_ip:VARCHAR",
		"contributor_pk:VARCHAR",
		"metro_pk:VARCHAR",
		"max_users:INTEGER",
	}
}

func (s *DeviceSchema) ToRow(d Device) []any {
	return []any{
		d.PK,
		d.Status,
		d.DeviceType,
		d.Code,
		d.PublicIP,
		d.ContributorPK,
		d.MetroPK,
		d.MaxUsers,
	}
}

func (s *DeviceSchema) GetPrimaryKey(d Device) string {
	return d.PK
}

// UserSchema defines the schema for users
type UserSchema struct{}

func (s *UserSchema) Name() string {
	return "dz_users"
}

func (s *UserSchema) PrimaryKeyColumns() []string {
	return []string{"pk:VARCHAR"}
}

func (s *UserSchema) PayloadColumns() []string {
	return []string{
		"owner_pubkey:VARCHAR",
		"status:VARCHAR",
		"kind:VARCHAR",
		"client_ip:VARCHAR",
		"dz_ip:VARCHAR",
		"device_pk:VARCHAR",
		"tunnel_id:INTEGER",
	}
}

func (s *UserSchema) ToRow(u User) []any {
	return []any{
		u.PK,
		u.OwnerPubkey,
		u.Status,
		u.Kind,
		u.ClientIP.String(),
		u.DZIP.String(),
		u.DevicePK,
		u.TunnelID,
	}
}

func (s *UserSchema) GetPrimaryKey(u User) string {
	return u.PK
}

// MetroSchema defines the schema for metros
type MetroSchema struct{}

func (s *MetroSchema) Name() string {
	return "dz_metros"
}

func (s *MetroSchema) PrimaryKeyColumns() []string {
	return []string{"pk:VARCHAR"}
}

func (s *MetroSchema) PayloadColumns() []string {
	return []string{
		"code:VARCHAR",
		"name:VARCHAR",
		"longitude:DOUBLE",
		"latitude:DOUBLE",
	}
}

func (s *MetroSchema) ToRow(m Metro) []any {
	return []any{
		m.PK,
		m.Code,
		m.Name,
		m.Longitude,
		m.Latitude,
	}
}

func (s *MetroSchema) GetPrimaryKey(m Metro) string {
	return m.PK
}

// LinkSchema defines the schema for links
type LinkSchema struct{}

func (s *LinkSchema) Name() string {
	return "dz_links"
}

func (s *LinkSchema) PrimaryKeyColumns() []string {
	return []string{"pk:VARCHAR"}
}

func (s *LinkSchema) PayloadColumns() []string {
	return []string{
		"status:VARCHAR",
		"code:VARCHAR",
		"tunnel_net:VARCHAR",
		"contributor_pk:VARCHAR",
		"side_a_pk:VARCHAR",
		"side_z_pk:VARCHAR",
		"side_a_iface_name:VARCHAR",
		"side_z_iface_name:VARCHAR",
		"link_type:VARCHAR",
		"committed_rtt_ns:BIGINT",
		"committed_jitter_ns:BIGINT",
		"bandwidth_bps:BIGINT",
		"isis_delay_override_ns:BIGINT",
	}
}

func (s *LinkSchema) ToRow(l Link) []any {
	return []any{
		l.PK,
		l.Status,
		l.Code,
		l.TunnelNet,
		l.ContributorPK,
		l.SideAPK,
		l.SideZPK,
		l.SideAIfaceName,
		l.SideZIfaceName,
		l.LinkType,
		l.CommittedRTTNs,
		l.CommittedJitterNs,
		l.Bandwidth,
		l.ISISDelayOverrideNs,
	}
}

func (s *LinkSchema) GetPrimaryKey(l Link) string {
	return l.PK
}

var (
	contributorSchema = &ContributorSchema{}
	deviceSchema      = &DeviceSchema{}
	userSchema        = &UserSchema{}
	metroSchema       = &MetroSchema{}
	linkSchema        = &LinkSchema{}
)

func NewContributorDataset(log *slog.Logger) (*dataset.DimensionType2Dataset, error) {
	return dataset.NewDimensionType2Dataset(log, contributorSchema)
}

func NewDeviceDataset(log *slog.Logger) (*dataset.DimensionType2Dataset, error) {
	return dataset.NewDimensionType2Dataset(log, deviceSchema)
}

func NewUserDataset(log *slog.Logger) (*dataset.DimensionType2Dataset, error) {
	return dataset.NewDimensionType2Dataset(log, userSchema)
}

func NewMetroDataset(log *slog.Logger) (*dataset.DimensionType2Dataset, error) {
	return dataset.NewDimensionType2Dataset(log, metroSchema)
}

func NewLinkDataset(log *slog.Logger) (*dataset.DimensionType2Dataset, error) {
	return dataset.NewDimensionType2Dataset(log, linkSchema)
}
