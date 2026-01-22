// Package oc contains ygot-generated Go structs from OpenConfig YANG models.
//
// This package is auto-generated from the following OpenConfig models (v5.4.0):
//   - openconfig-network-instance (includes ISIS, BGP, routing protocols)
//   - openconfig-interfaces
//   - openconfig-system
//   - openconfig-platform (components)
//   - openconfig-platform-transceiver (optical transceiver state)
//
// # Regenerating
//
// To regenerate this package, run:
//
//	cd telemetry/gnmi-writer/internal/gnmi/oc/generate
//	./generate.sh
//
// Or build and run the Docker image manually:
//
//	docker build -t ygot-generator -f Dockerfile .
//	docker run --rm ygot-generator > ../oc.go
//
// # Usage
//
// Use ytypes.UnmarshalNotifications to unmarshal gNMI notifications:
//
//	schema, err := oc.Schema()
//	if err != nil {
//	    return err
//	}
//
//	err = ytypes.UnmarshalNotifications(schema, notifications)
//	if err != nil {
//	    return err
//	}
//
//	// Access data through schema.Root
//	device := schema.Root.(*oc.Device)
//	for name, ni := range device.NetworkInstance {
//	    // Access ISIS adjacencies
//	    isis := ni.Protocol["ISIS"]["1"].Isis
//	    for ifID, iface := range isis.Interface {
//	        for level, lvl := range iface.Level {
//	            for sysID, adj := range lvl.Adjacency {
//	                fmt.Printf("Adjacency: %s state=%s\n", sysID, adj.AdjacencyState)
//	            }
//	        }
//	    }
//	}
package oc
