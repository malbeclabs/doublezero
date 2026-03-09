package config

const (
	// TWAMPListenPort is the port on which devices listen for TWAMP probes.
	TWAMPListenPort = 862

	// DefaultGeoprobeUDPPort is the default UDP offset port for geoprobe-agents.
	DefaultGeoprobeUDPPort = 8923

	// DefaultGeoprobeTWAMPPort is the default TWAMP reflector port for geoprobe-agents.
	DefaultGeoprobeTWAMPPort = 8925

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
