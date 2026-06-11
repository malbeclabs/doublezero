package controller

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeaturesConfigLoad(t *testing.T) {
	yaml := `
features:
  flex_algo:
    enabled: true
    link_tagging:
      exclude:
        links:
          - ABC123pubkey
    community_stamping:
      all: false
      tenants:
        - TenantPubkey1
      devices:
        - DevicePubkey1
      exclude:
        devices:
          - ExcludedDevicePubkey
`
	config, err := LoadFeaturesConfig(strings.NewReader(yaml))
	require.NoError(t, err)
	assert.True(t, config.Features.FlexAlgo.Enabled)
	assert.Len(t, config.Features.FlexAlgo.LinkTagging.Exclude.Links, 1)
	assert.Equal(t, "ABC123pubkey", config.Features.FlexAlgo.LinkTagging.Exclude.Links[0])
	assert.False(t, config.Features.FlexAlgo.CommunityStamping.All)
	assert.Len(t, config.Features.FlexAlgo.CommunityStamping.Tenants, 1)
	assert.Len(t, config.Features.FlexAlgo.CommunityStamping.Devices, 1)
	assert.Len(t, config.Features.FlexAlgo.CommunityStamping.Exclude.Devices, 1)
}

func TestFeaturesConfigEmpty(t *testing.T) {
	yaml := `features: {}`
	config, err := LoadFeaturesConfig(strings.NewReader(yaml))
	require.NoError(t, err)
	assert.False(t, config.Features.FlexAlgo.Enabled)
	assert.Empty(t, config.Features.FlexAlgo.LinkTagging.Exclude.Links)
}

func TestLinkTaggingIsExcluded(t *testing.T) {
	cfg := LinkTaggingConfig{}
	cfg.Exclude.Links = []string{"pubkey1", "pubkey2"}
	assert.True(t, cfg.IsExcluded("pubkey1"))
	assert.True(t, cfg.IsExcluded("pubkey2"))
	assert.False(t, cfg.IsExcluded("pubkey3"))
	assert.False(t, cfg.IsExcluded(""))
}

func TestShouldStamp(t *testing.T) {
	cfg := CommunityStampingConfig{
		All:     false,
		Tenants: []string{"tenant1"},
		Devices: []string{"device1"},
	}
	cfg.Exclude.Devices = []string{"excluded_device"}

	// tenant in list, device not excluded → stamp
	assert.True(t, cfg.ShouldStamp("tenant1", "any_device"))
	// device in list, tenant not in list → stamp
	assert.True(t, cfg.ShouldStamp("other_tenant", "device1"))
	// excluded device — always false regardless of tenant/device match
	assert.False(t, cfg.ShouldStamp("tenant1", "excluded_device"))
	// not in any list
	assert.False(t, cfg.ShouldStamp("other_tenant", "other_device"))
}

func TestShouldStampAllTrue(t *testing.T) {
	cfg := CommunityStampingConfig{All: true}
	assert.True(t, cfg.ShouldStamp("any_tenant", "any_device"))

	cfg.Exclude.Devices = []string{"excluded"}
	assert.False(t, cfg.ShouldStamp("any_tenant", "excluded"))
	assert.True(t, cfg.ShouldStamp("any_tenant", "other_device"))
}

func TestShouldStampExcludeOverridesAll(t *testing.T) {
	cfg := CommunityStampingConfig{
		All:     true,
		Tenants: []string{"tenant1"},
		Devices: []string{"device1"},
	}
	cfg.Exclude.Devices = []string{"device1"}
	// device1 is both in Devices and Exclude.Devices — exclude wins
	assert.False(t, cfg.ShouldStamp("tenant1", "device1"))
}
