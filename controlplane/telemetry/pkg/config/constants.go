package config

const (
	// TWAMPListenPort is the port on which devices listen for TWAMP probes.
	TWAMPListenPort = 862

	// InternetTelemetryDataProviderNameRIPEAtlas is the name of the RIPE Atlas data provider.
	InternetTelemetryDataProviderNameRIPEAtlas = "ripeatlas"

	// InternetTelemetryDataProviderNameWheresitup is the name of the WhereIsItUp data provider.
	InternetTelemetryDataProviderNameWheresitup = "wheresitup"
)

var (
	InternetTelemetryDataProviders = []string{
		InternetTelemetryDataProviderNameRIPEAtlas,
		InternetTelemetryDataProviderNameWheresitup,
	}
)
