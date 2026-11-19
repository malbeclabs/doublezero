package shreds

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unsafe"
)

// deserializeAccount validates the discriminator and deserializes the account
// data into the given struct type. Tolerates trailing bytes for forward
// compatibility.
func deserializeAccount[T any](data []byte, disc [8]byte) (*T, error) {
	if err := validateDiscriminator(data, disc); err != nil {
		return nil, err
	}
	body := data[discriminatorSize:]
	var zero T
	need := int(unsafe.Sizeof(zero))
	if len(body) < need {
		return nil, fmt.Errorf("account data too short: have %d bytes, need at least %d", len(body), need)
	}
	var item T
	if err := binary.Read(bytes.NewReader(body[:need]), binary.LittleEndian, &item); err != nil {
		return nil, fmt.Errorf("deserializing account: %w", err)
	}
	return &item, nil
}

// DeserializeFeedDistribution decodes a FeedDistribution account from raw
// account data, discriminator included.
func DeserializeFeedDistribution(data []byte) (*FeedDistribution, error) {
	return deserializeAccount[FeedDistribution](data, DiscriminatorFeedDistribution)
}
