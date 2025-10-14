package serviceability

import (
	"fmt"
	"net"
	"reflect"
	"sort"
	"strings"
	"time"

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
			if slice, ok := fieldValue.Interface().([][5]uint8); ok {
				var prefixes []string
				for _, p := range slice {
					ip := net.IP(p[0:4])
					mask := int(p[4])
					prefixes = append(prefixes, fmt.Sprintf("%s/%d", ip.String(), mask))
				}
				finalValue = strings.Join(prefixes, ",")
			} else {
				return "", fmt.Errorf("field '%s' tagged as 'cidr' but is not [][5]uint8", fieldType.Name)
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
			fields[name] = finalValue
		}
	}

	// Merge additional tags, allowing them to override any existing tags from the struct.
	for k, v := range additionalTags {
		tags[k] = v
	}

	if measurement == "" {
		return "", fmt.Errorf("measurement name cannot be empty")
	}

	var tagParts []string
	for k, v := range tags {
		tagParts = append(tagParts, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(tagParts)

	var fieldParts []string
	for k, v := range fields {
		if s, ok := v.(string); ok {
			fieldParts = append(fieldParts, fmt.Sprintf(`%s="%s"`, k, s))
		} else {
			fieldParts = append(fieldParts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	sort.Strings(fieldParts)

	tagStr := strings.Join(tagParts, ",")
	fieldStr := strings.Join(fieldParts, ",")
	timestampStr := fmt.Sprintf("%d", ts.UnixNano())

	var builder strings.Builder
	builder.WriteString(measurement)

	if tagStr != "" {
		builder.WriteByte(',')
		builder.WriteString(tagStr)
	}

	if fieldStr != "" {
		builder.WriteByte(' ')
		builder.WriteString(fieldStr)
	}

	builder.WriteByte(' ')
	builder.WriteString(timestampStr)

	return builder.String(), nil
}
