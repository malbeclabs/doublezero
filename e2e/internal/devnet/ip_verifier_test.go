package devnet

import "testing"

func TestIPVerifierSpecValidate(t *testing.T) {
	cyoa := CYOANetworkSpec{CIDRPrefix: 24}

	t.Run("disabled spec is left alone", func(t *testing.T) {
		// Nothing is defaulted for a disabled verifier, so a devnet that does not run one never
		// derives a CYOA address for it and never points clients anywhere.
		spec := IPVerifierSpec{Disabled: true}
		if err := spec.Validate(cyoa); err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
		if spec.CYOANetworkIPHostID != 0 {
			t.Errorf("CYOANetworkIPHostID = %d, want 0", spec.CYOANetworkIPHostID)
		}
	})

	t.Run("defaults the CYOA host ID", func(t *testing.T) {
		// The zero value runs the verifier: every devnet gets one unless it opts out.
		spec := IPVerifierSpec{}
		if err := spec.Validate(cyoa); err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
		if spec.CYOANetworkIPHostID != defaultIPVerifierCYOANetworkIPHostID {
			t.Errorf("CYOANetworkIPHostID = %d, want %d", spec.CYOANetworkIPHostID, defaultIPVerifierCYOANetworkIPHostID)
		}
	})

	t.Run("rejects a host ID outside the subnet", func(t *testing.T) {
		// 256 does not fit a /24. Catching it here beats a Docker IPAM error at start time.
		spec := IPVerifierSpec{CYOANetworkIPHostID: 256}
		if err := spec.Validate(cyoa); err == nil {
			t.Fatal("Validate() = nil, want an out-of-range error")
		}
	})

	t.Run("rejects the broadcast host ID", func(t *testing.T) {
		// The bound the client and device specs use. 255 is the broadcast host ID of a /24, and
		// an earlier version of this check accepted it while its error message claimed otherwise.
		spec := IPVerifierSpec{CYOANetworkIPHostID: 255}
		if err := spec.Validate(cyoa); err == nil {
			t.Fatal("Validate() = nil, want the broadcast address rejected")
		}
	})

	t.Run("accepts the last usable host ID", func(t *testing.T) {
		// The other side of that bound: 254 must still pass, so the fix did not go one too far.
		spec := IPVerifierSpec{CYOANetworkIPHostID: 254}
		if err := spec.Validate(cyoa); err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
		if spec.CYOANetworkIPHostID != 254 {
			t.Errorf("CYOANetworkIPHostID = %d, want it left alone", spec.CYOANetworkIPHostID)
		}
	})

	t.Run("rejects a relative keypair path", func(t *testing.T) {
		// The path is handed to Docker as a host mount source, which only resolves absolutely.
		spec := IPVerifierSpec{KeypairPath: "ip-verifier-keypair.json"}
		if err := spec.Validate(cyoa); err == nil {
			t.Fatal("Validate() = nil, want an absolute-path error")
		}
	})
}
