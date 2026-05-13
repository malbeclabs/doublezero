package controller

import (
	"net"
	"testing"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

func TestIsBgpMartian(t *testing.T) {
	tests := []struct {
		name      string
		ip        string
		isMartian bool
	}{
		// Martian addresses
		{"zero network", "0.0.0.1", true},
		{"private 10.x", "10.0.0.1", true},
		{"private 10.x deep", "10.255.255.255", true},
		{"shared address space", "100.64.0.1", true},
		{"loopback", "127.0.0.1", true},
		{"link-local", "169.254.1.1", true},
		{"private 172.16.x", "172.16.0.1", true},
		{"private 172.31.x", "172.31.255.255", true},
		{"documentation TEST-NET-1", "192.0.2.1", true},
		{"private 192.168.x", "192.168.1.1", true},
		{"documentation TEST-NET-2", "198.51.100.1", true},
		{"documentation TEST-NET-3", "203.0.113.1", true},
		{"multicast", "224.0.0.1", true},
		{"multicast high", "239.255.255.255", true},
		{"reserved class E", "240.0.0.1", true},
		{"broadcast", "255.255.255.255", true},

		// Non-martian addresses
		{"valid public 1", "1.1.1.1", false},
		{"valid public 8", "8.8.8.8", false},
		{"valid public 100", "100.128.0.1", false},
		{"valid public 148", "148.51.120.1", false},
		{"valid public 172.15", "172.15.255.255", false},
		{"valid public 172.32", "172.32.0.0", false},
		{"valid public 192.0.3", "192.0.3.1", false},
		{"valid public 223", "223.255.255.255", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip).To4()
			if ip == nil {
				t.Fatalf("failed to parse IP %s", tt.ip)
			}
			got := isBgpMartian(ip)
			if got != tt.isMartian {
				t.Errorf("isBgpMartian(%s) = %v, want %v", tt.ip, got, tt.isMartian)
			}
		})
	}
}

func TestMtuForInterface(t *testing.T) {
	tests := []struct {
		name string
		in   Interface
		want uint16
	}{
		{"plain fabric physical", Interface{InterfaceType: InterfaceTypePhysical}, InterfaceMtu},
		{"CYOA", Interface{InterfaceType: InterfaceTypePhysical, IsCYOA: true}, CyoaDiaInterfaceMtu},
		{"DIA", Interface{InterfaceType: InterfaceTypePhysical, IsDIA: true}, CyoaDiaInterfaceMtu},
		{"CYOA and DIA both set", Interface{IsCYOA: true, IsDIA: true}, CyoaDiaInterfaceMtu},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mtuForInterface(tt.in); got != tt.want {
				t.Errorf("mtuForInterface(%+v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestToInterface_RoleBasedMTU(t *testing.T) {
	// toInterface must compute MTU from role and ignore the onchain Mtu field
	// entirely. This guards against V1-deserialized interfaces (Mtu = 0) and
	// any stale onchain MTU.
	tests := []struct {
		name      string
		iface     serviceability.Interface
		wantMtu   uint16
		wantCYOA  bool
		wantDIA   bool
		wantIsSub bool
	}{
		{
			name: "fabric physical, onchain Mtu=0",
			iface: serviceability.Interface{
				Name:          "Switch1/1/1",
				InterfaceType: serviceability.InterfaceTypePhysical,
				IpNet:         [5]uint8{172, 16, 0, 0, 31},
				Mtu:           0,
			},
			wantMtu: InterfaceMtu,
		},
		{
			name: "fabric physical, onchain Mtu=2048",
			iface: serviceability.Interface{
				Name:          "Switch1/1/1",
				InterfaceType: serviceability.InterfaceTypePhysical,
				IpNet:         [5]uint8{172, 16, 0, 0, 31},
				Mtu:           2048,
			},
			wantMtu: InterfaceMtu,
		},
		{
			name: "fabric subinterface, onchain Mtu=9216",
			iface: serviceability.Interface{
				Name:          "Switch1/1/2.100",
				InterfaceType: serviceability.InterfaceTypePhysical,
				VlanId:        100,
				IpNet:         [5]uint8{172, 16, 0, 2, 31},
				Mtu:           9216,
			},
			wantMtu:   InterfaceMtu,
			wantIsSub: true,
		},
		{
			name: "CYOA physical, onchain Mtu=0",
			iface: serviceability.Interface{
				Name:          "Switch1/1/5",
				InterfaceType: serviceability.InterfaceTypePhysical,
				InterfaceCYOA: serviceability.InterfaceCYOAGREOverFabric,
				IpNet:         [5]uint8{172, 16, 0, 14, 31},
				Mtu:           0,
			},
			wantMtu:  CyoaDiaInterfaceMtu,
			wantCYOA: true,
		},
		{
			name: "DIA physical, onchain Mtu=9216",
			iface: serviceability.Interface{
				Name:          "Switch1/1/6",
				InterfaceType: serviceability.InterfaceTypePhysical,
				InterfaceDIA:  serviceability.InterfaceDIADIA,
				IpNet:         [5]uint8{172, 16, 0, 16, 31},
				Mtu:           9216,
			},
			wantMtu: CyoaDiaInterfaceMtu,
			wantDIA: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toInterface(tt.iface)
			if err != nil {
				t.Fatalf("toInterface(%+v) returned error: %v", tt.iface, err)
			}
			if got.Mtu != tt.wantMtu {
				t.Errorf("Mtu = %d, want %d (role-based, ignoring onchain %d)", got.Mtu, tt.wantMtu, tt.iface.Mtu)
			}
			if got.IsCYOA != tt.wantCYOA {
				t.Errorf("IsCYOA = %v, want %v", got.IsCYOA, tt.wantCYOA)
			}
			if got.IsDIA != tt.wantDIA {
				t.Errorf("IsDIA = %v, want %v", got.IsDIA, tt.wantDIA)
			}
			if got.IsSubInterface != tt.wantIsSub {
				t.Errorf("IsSubInterface = %v, want %v", got.IsSubInterface, tt.wantIsSub)
			}
		})
	}
}

func TestInterface_GetParent_DoesNotCopyMTU(t *testing.T) {
	// Parent MTU is set in processDeviceInterfacesAndPeers (max of subinterface
	// MTUs); GetParent must not copy the child's MTU.
	child := Interface{
		Name:           "Switch1/1/2.100",
		IsSubInterface: true,
		InterfaceType:  InterfaceTypePhysical,
		Mtu:            InterfaceMtu,
	}
	parent, err := child.GetParent()
	if err != nil {
		t.Fatalf("GetParent returned error: %v", err)
	}
	if parent.Name != "Switch1/1/2" {
		t.Errorf("parent Name = %q, want %q", parent.Name, "Switch1/1/2")
	}
	if !parent.IsSubInterfaceParent {
		t.Errorf("parent IsSubInterfaceParent = false, want true")
	}
	if parent.Mtu != 0 {
		t.Errorf("parent Mtu = %d, want 0 (parent MTU is set by the dedup loop, not copied from child)", parent.Mtu)
	}
}
