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

// Info is what the server says of itself, to a client asking over HTTP or on the network.
type Info struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}
