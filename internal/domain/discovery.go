package domain

import (
	"fmt"
	"slices"
)

// Discovery is whether the server answers clients looking for it on the local network.
type Discovery string

const (
	DiscoveryBroadcast Discovery = "broadcast"
	DiscoveryOff       Discovery = "off"
)

func Discoveries() []Discovery {
	return []Discovery{DiscoveryBroadcast, DiscoveryOff}
}

func ParseDiscovery(s string) (Discovery, error) {
	if d := Discovery(s); slices.Contains(Discoveries(), d) {
		return d, nil
	}
	return "", fmt.Errorf("discovery %q is not one of %v", s, Discoveries())
}
