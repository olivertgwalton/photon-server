package domain

// Discovery is whether the server answers clients looking for it on the local network.
type Discovery string

const (
	DiscoveryBroadcast Discovery = "broadcast"
	DiscoveryOff       Discovery = "off"
)

func Discoveries() []Discovery {
	return []Discovery{DiscoveryBroadcast, DiscoveryOff}
}
