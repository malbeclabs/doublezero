package docker

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

const (
	defaultRetries = 10
)

// SubnetAllocator is a helper for allocating subnets from a base CIDR.
//
// It can be used to allocate subnets for testing purposes, and is thread-safe using an in-memory
// lock, so it can be used in parallel. It also checks for overlaps with existing subnets in the
// docker network using a docker client, and will retry with a different salt if the subnet is
// already in use.
type SubnetAllocator struct {
	Base       net.IP
	BaseMask   int
	SubnetMask int
	MaxSubnets int
	docker     *client.Client
	mu         sync.Mutex
}

func NewSubnetAllocator(baseCIDR string, subnetMask int, docker *client.Client) *SubnetAllocator {
	_, ipnet, err := net.ParseCIDR(baseCIDR)
	if err != nil {
		panic(err)
	}
	ones, bits := ipnet.Mask.Size()
	subnetBits := subnetMask - ones
	if subnetBits < 0 || subnetMask > bits {
		panic("invalid subnet mask")
	}
	return &SubnetAllocator{
		Base:       ipnet.IP,
		BaseMask:   ones,
		SubnetMask: subnetMask,
		MaxSubnets: 1 << subnetBits,
		docker:     docker,
	}
}

func NewDefaultSubnetAllocator(docker *client.Client) *SubnetAllocator {
	return NewSubnetAllocator("10.128.0.0/9", 24, docker)
}

func (a *SubnetAllocator) GetSubnetCIDR(testID string, salt int) (string, error) {
	h := sha256.New()
	h.Write([]byte(testID))
	h.Write([]byte{byte(salt >> 24), byte(salt >> 16), byte(salt >> 8), byte(salt)})
	hash := h.Sum(nil)
	idx := binary.BigEndian.Uint32(hash[:4]) % uint32(a.MaxSubnets)

	base := a.Base.To4()
	if base == nil {
		return "", errors.New("only IPv4 is supported")
	}
	offset := idx << (32 - a.SubnetMask)
	subnetIP := make(net.IP, 4)
	copy(subnetIP, base)
	for i := 0; i < 4; i++ {
		subnetIP[3-i] += byte(offset >> (8 * i))
	}
	return fmt.Sprintf("%s/%d", subnetIP, a.SubnetMask), nil
}

func (a *SubnetAllocator) FindAvailableSubnet(ctx context.Context, testID string) (string, error) {
	for salt := 0; salt < defaultRetries; salt++ {
		sn, err := a.GetSubnetCIDR(testID, salt)
		if err != nil {
			return "", err
		}

		a.mu.Lock()
		inUse, err := a.isSubnetInUseLocked(ctx, sn)
		a.mu.Unlock()
		if err != nil {
			return "", err
		}
		if !inUse {
			return sn, nil
		}
	}
	return "", errors.New("no available subnet found after retries")
}

func (a *SubnetAllocator) isSubnetInUseLocked(ctx context.Context, subnet string) (bool, error) {
	nets, err := a.docker.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return false, err
	}
	_, desiredCIDR, err := net.ParseCIDR(subnet)
	if err != nil {
		return false, err
	}
	for _, netInfo := range nets {
		for _, ipam := range netInfo.IPAM.Config {
			if ipam.Subnet == "" {
				continue
			}
			_, existingCIDR, err := net.ParseCIDR(ipam.Subnet)
			if err != nil {
				continue
			}
			if cidrOverlap(existingCIDR, desiredCIDR) {
				return true, nil
			}
		}
	}
	return false, nil
}

func cidrOverlap(a, b *net.IPNet) bool {
	return a.Contains(b.IP) || b.Contains(a.IP)
}
