package devnet

import "testing"

// CI pulls Prometheus and InfluxDB from GHCR and sets these variables. If a spec
// ignores them, testcontainers pulls the upstream ref anonymously again.
func TestThirdPartyImageOverride(t *testing.T) {
	cases := []struct {
		name     string
		envVar   string
		fallback string
		validate func() (string, error)
	}{
		{"prometheus", "DZ_PROMETHEUS_IMAGE", prometheusImage, func() (string, error) {
			s := PrometheusSpec{}
			err := s.Validate()
			return s.ContainerImage, err
		}},
		{"influxdb", "DZ_INFLUXDB_IMAGE", influxDBImage, func() (string, error) {
			s := InfluxDBSpec{}
			err := s.Validate()
			return s.ContainerImage, err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name+" uses the upstream image when unset", func(t *testing.T) {
			t.Setenv(tc.envVar, "")
			got, err := tc.validate()
			if err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if got != tc.fallback {
				t.Errorf("ContainerImage = %q, want %q", got, tc.fallback)
			}
		})
		t.Run(tc.name+" uses the env image when set", func(t *testing.T) {
			want := "ghcr.io/example/" + tc.name + "@sha256:0000"
			t.Setenv(tc.envVar, want)
			got, err := tc.validate()
			if err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if got != want {
				t.Errorf("ContainerImage = %q, want %q", got, want)
			}
		})
	}
}
