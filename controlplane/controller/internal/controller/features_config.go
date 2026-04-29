package controller

import (
	"io"
	"slices"

	"gopkg.in/yaml.v3"
)

// FeaturesConfig is optionally loaded from /etc/doublezero-controller/features.yaml at
// controller startup. If the file is absent the controller runs with all features disabled.
// It gates flex-algo topology config, link tagging, and BGP color community stamping.
type FeaturesConfig struct {
	Features struct {
		FlexAlgo FlexAlgoConfig `yaml:"flex_algo"`
	} `yaml:"features"`
}

// FlexAlgoConfig controls IS-IS Flex-Algo and BGP color extended community behaviour.
type FlexAlgoConfig struct {
	Enabled           bool                    `yaml:"enabled"`
	LinkTagging       LinkTaggingConfig       `yaml:"link_tagging"`
	CommunityStamping CommunityStampingConfig `yaml:"community_stamping"`
}

// LinkTaggingConfig controls which links receive IS-IS TE admin-group attributes.
type LinkTaggingConfig struct {
	Exclude struct {
		Links []string `yaml:"links"` // link pubkeys to skip
	} `yaml:"exclude"`
}

// IsExcluded returns true if the given link pubkey is in the exclude list.
func (c *LinkTaggingConfig) IsExcluded(linkPubKey string) bool {
	return slices.Contains(c.Exclude.Links, linkPubKey)
}

// CommunityStampingConfig controls BGP color extended community stamping per tenant/device.
// A device is stamped if All is true, OR its pubkey is in Devices, OR the tenant's pubkey
// is in Tenants — unless the device pubkey is in Exclude.Devices (overrides all).
type CommunityStampingConfig struct {
	All     bool     `yaml:"all"`
	Tenants []string `yaml:"tenants"` // tenant pubkeys
	Devices []string `yaml:"devices"` // device pubkeys
	Exclude struct {
		Devices []string `yaml:"devices"`
	} `yaml:"exclude"`
}

// ShouldStamp returns true if BGP color communities should be stamped for the
// given (tenantPubKey, devicePubKey) pair.
func (c *CommunityStampingConfig) ShouldStamp(tenantPubKey, devicePubKey string) bool {
	if slices.Contains(c.Exclude.Devices, devicePubKey) {
		return false
	}
	if c.All {
		return true
	}
	return slices.Contains(c.Tenants, tenantPubKey) || slices.Contains(c.Devices, devicePubKey)
}

// LoadFeaturesConfig parses a features YAML config from the given reader.
func LoadFeaturesConfig(r io.Reader) (*FeaturesConfig, error) {
	var cfg FeaturesConfig
	if err := yaml.NewDecoder(r).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
