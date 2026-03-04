package geolocation

import "fmt"

// DeserializeProgramConfig deserializes binary data into a GeolocationProgramConfig.
// It validates the account type discriminator before decoding.
func DeserializeProgramConfig(data []byte) (*GeolocationProgramConfig, error) {
	if len(data) < 1 {
		return nil, fmt.Errorf("account data too short: %d bytes", len(data))
	}
	if AccountType(data[0]) != AccountTypeProgramConfig {
		return nil, fmt.Errorf("unexpected account type: got %d, want %d", data[0], AccountTypeProgramConfig)
	}

	var config GeolocationProgramConfig
	if err := config.Deserialize(data); err != nil {
		return nil, fmt.Errorf("failed to deserialize program config: %w", err)
	}
	return &config, nil
}

// DeserializeGeoProbe deserializes binary data into a GeoProbe.
// It validates the account type discriminator before decoding.
func DeserializeGeoProbe(data []byte) (*GeoProbe, error) {
	if len(data) < 1 {
		return nil, fmt.Errorf("account data too short: %d bytes", len(data))
	}
	if AccountType(data[0]) != AccountTypeGeoProbe {
		return nil, fmt.Errorf("unexpected account type: got %d, want %d", data[0], AccountTypeGeoProbe)
	}

	var probe GeoProbe
	if err := probe.Deserialize(data); err != nil {
		return nil, fmt.Errorf("failed to deserialize geo probe: %w", err)
	}
	return &probe, nil
}
