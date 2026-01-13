package gnmi

import (
	"fmt"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/ygot/ytypes"

	"github.com/malbeclabs/doublezero/telemetry/gnmi-writer/internal/gnmi/oc"
)

// listSchemaCache is no longer needed with uncompressed paths.
// Kept for backwards compatibility but unused.
type listSchemaCache map[string]string

// buildListSchemaCache is no longer needed with uncompressed paths.
func buildListSchemaCache(schema *ytypes.Schema) listSchemaCache {
	return make(listSchemaCache)
}

// unmarshalNotification unmarshals a gNMI notification update into an oc.Device.
// With uncompressed paths (-compress_paths=false), the gNMI paths match the schema
// directly, so we can use SetNode without any special handling.
func (p *Processor) unmarshalNotification(notification *gpb.Notification, update *gpb.Update) (*oc.Device, error) {
	device := &oc.Device{}
	fullPath := mergePaths(notification.GetPrefix(), update.GetPath())
	val := update.GetVal()

	err := ytypes.SetNode(
		p.schema.SchemaTree["Device"],
		device,
		fullPath,
		val,
		&ytypes.InitMissingElements{},
		&ytypes.IgnoreExtraFields{},
	)
	if err != nil {
		return nil, fmt.Errorf("SetNode failed: %w", err)
	}

	return device, nil
}

// mergePaths combines a prefix path and an update path into a single path.
func mergePaths(prefix, path *gpb.Path) *gpb.Path {
	if prefix == nil {
		return path
	}
	if path == nil {
		return prefix
	}

	result := &gpb.Path{
		Origin: prefix.GetOrigin(),
		Target: prefix.GetTarget(),
		Elem:   make([]*gpb.PathElem, 0, len(prefix.GetElem())+len(path.GetElem())),
	}
	result.Elem = append(result.Elem, prefix.GetElem()...)
	result.Elem = append(result.Elem, path.GetElem()...)
	return result
}
