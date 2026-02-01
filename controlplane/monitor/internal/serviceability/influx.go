package serviceability

import (
	"fmt"
	"maps"
	"net"
	"reflect"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/mr-tron/base58"
)

// ToLineProtocol converts a struct to InfluxDB line protocol format via struct tags. The tag format
// is `influx:"tag|field[,name][,option...]"` where `option` can be `pubkey`, `ip`, or `cidr`:
//
//   - The `pubkey` option encodes a [32]byte field as a base58 string.
//   - The `ip` option formats a [4]uint8 field as a dotted-decimal IP address.
//   - The `cidr` option formats a slice of [5]uint8 (first 4 bytes are the IP, 5th byte is the mask) as
//     comma-separated list of CIDR strings.
func ToLineProtocol(measurement string, s any, ts time.Time, additionalTags map[string]string) (string, error) {
	v := reflect.ValueOf(s)
	if v.Kind() != reflect.Struct {
		return "", fmt.Errorf("input must be a struct")
	}
	t := v.Type()

	tags := make(map[string]string)
	fields := make(map[string]any)

	for i := 0; i < v.NumField(); i++ {
		fieldValue := v.Field(i)
		fieldType := t.Field(i)
		tag := fieldType.Tag.Get("influx")

		if tag == "" || tag == "-" {
			continue
		}

		parts := strings.Split(tag, ",")
		tagType := parts[0]
		name := fieldType.Name
		if len(parts) > 1 && parts[1] != "" {
			name = parts[1]
		}

		hasPubkeyOption, hasIpOption, hasCidrOption := false, false, false
		if len(parts) > 2 {
			for _, option := range parts[2:] {
				switch option {
				case "pubkey":
					hasPubkeyOption = true
				case "ip":
					hasIpOption = true
				case "cidr":
					hasCidrOption = true
				}
			}
		}

		var finalValue any
		if hasPubkeyOption {
			if arr, ok := fieldValue.Interface().([32]byte); ok {
				finalValue = base58.Encode(arr[:])
			} else {
				return "", fmt.Errorf("field '%s' tagged as 'pubkey' but is not [32]byte", fieldType.Name)
			}
		} else if hasIpOption {
			if arr, ok := fieldValue.Interface().([4]uint8); ok {
				finalValue = net.IP(arr[:]).String()
			} else {
				return "", fmt.Errorf("field '%s' tagged as 'ip' but is not [4]uint8", fieldType.Name)
			}
		} else if hasCidrOption {
			switch v := fieldValue.Interface().(type) {
			case [][5]uint8:
				var prefixes []string
				for _, p := range v {
					mask := int(p[4])
					// Mirror onChainNetToString: skip invalid or zero prefix
					if mask <= 0 || mask > 32 {
						continue
					}
					ip := net.IP(p[0:4])
					prefixes = append(prefixes, fmt.Sprintf("%s/%d", ip.String(), mask))
				}
				finalValue = strings.Join(prefixes, ",")
			case [5]uint8:
				mask := int(v[4])
				if mask <= 0 || mask > 32 {
					// invalid/zero prefix -> treat as unset
					finalValue = ""
				} else {
					ip := net.IP(v[0:4])
					finalValue = fmt.Sprintf("%s/%d", ip.String(), mask)
				}
			default:
				return "", fmt.Errorf("field '%s' tagged as 'cidr' but is not [][5]uint8 or [5]uint8", fieldType.Name)
			}
		} else {
			finalValue = fieldValue.Interface()
		}

		switch tagType {
		case "tag":
			value := fmt.Sprintf("%v", finalValue)
			if value == "" {
				continue
			}
			tags[name] = value
		case "field":
			// The influx schemas have been initialized with float64 for numeric fields, so we
			// need to convert to float64 if the value is numeric.
			fields[name] = toFloatIfNumeric(finalValue)
		}
	}

	// Merge additional tags, allowing them to override any existing tags from the struct.
	maps.Copy(tags, additionalTags)

	if measurement == "" {
		return "", fmt.Errorf("measurement name cannot be empty")
	}

	p := influxdb2.NewPoint(measurement, tags, fields, ts)
	line := write.PointToLineProtocol(p, time.Nanosecond)
	line = strings.TrimSpace(line)
	return line, nil
}

func toFloatIfNumeric(v any) any {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int8:
		return float64(n)
	case int16:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case uint:
		return float64(n)
	case uint8:
		return float64(n)
	case uint16:
		return float64(n)
	case uint32:
		return float64(n)
	case uint64:
		return float64(n)
	case float32:
		return float64(n)
	case float64:
		return n
	default:
		return v
	}
}
