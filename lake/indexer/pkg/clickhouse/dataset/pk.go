package dataset

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"reflect"
	"time"
)

type NaturalKey struct {
	Values []any
}

type SurrogateKey string

func NewNaturalKey(values ...any) *NaturalKey {
	return &NaturalKey{
		Values: values,
	}
}

// ToSurrogate converts a natural key to a deterministic surrogate key.
// Uses a length-delimited encoding to avoid collisions from fmt.Sprintf("%v") and "|" separator.
// Format: typeTag + ":" + length + ":" + payload for each value, then hash.
func (p *NaturalKey) ToSurrogate() SurrogateKey {
	var buf bytes.Buffer
	for _, val := range p.Values {
		if val == nil {
			buf.WriteString("nil:0:")
			continue
		}

		valType := reflect.TypeOf(val)
		typeTag := valType.String()

		// Serialize value deterministically based on type
		var payload []byte
		switch v := val.(type) {
		case string:
			payload = []byte(v)
		case int, int8, int16, int32, int64:
			var b [8]byte
			binary.BigEndian.PutUint64(b[:], uint64(reflect.ValueOf(v).Int()))
			payload = b[:]
		case uint, uint8, uint16, uint32, uint64:
			var b [8]byte
			binary.BigEndian.PutUint64(b[:], reflect.ValueOf(v).Uint())
			payload = b[:]
		case float32:
			var b [4]byte
			binary.BigEndian.PutUint32(b[:], math.Float32bits(v))
			payload = b[:]
		case float64:
			var b [8]byte
			binary.BigEndian.PutUint64(b[:], math.Float64bits(v))
			payload = b[:]
		case bool:
			if v {
				payload = []byte{1}
			} else {
				payload = []byte{0}
			}
		case time.Time:
			// Use RFC3339Nano for deterministic time encoding
			payload = []byte(v.UTC().Format(time.RFC3339Nano))
		default:
			// Fallback to string representation for unknown types
			// This is less ideal but maintains backward compatibility
			payload = []byte(fmt.Sprintf("%v", v))
		}

		// Write: typeTag:length:payload
		buf.WriteString(typeTag)
		buf.WriteString(":")
		buf.WriteString(fmt.Sprintf("%d", len(payload)))
		buf.WriteString(":")
		buf.Write(payload)
	}

	hash := sha256.Sum256(buf.Bytes())
	return SurrogateKey(hex.EncodeToString(hash[:]))
}
