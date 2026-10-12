package playback

import (
	"github.com/olivertgwalton/photon-server/internal/domain"
)

// RemoteLimit is the most a stream to a client on network is sent at, in kbps: the server's limit
// on a remote stream, else none, 0.
func RemoteLimit(n domain.Network, network domain.ClientNetwork) int {
	switch network {
	case domain.NetworkRemote:
		return n.RemoteMaxBitrateKbps
	case domain.NetworkLocal:
	}
	return 0
}

// Capped is a client's most bitrate within a limit, either 0 for none.
func Capped(kbps, limit int) int {
	if limit == 0 || kbps > 0 && kbps < limit {
		return kbps
	}
	return limit
}
