package playback

import (
	"context"
	"net/netip"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/peer"
)

type networkSettings interface {
	Network(ctx context.Context) (domain.Network, error)
}

// RemoteLimit is the most a stream to a client at addr is sent at, in kbps: the server's limit on
// a remote stream where addr is not local, else none, 0.
func RemoteLimit(ctx context.Context, settings networkSettings, addr netip.Addr) (int, error) {
	if peer.Local(addr) {
		return 0, nil
	}
	n, err := settings.Network(ctx)
	return n.RemoteMaxBitrateKbps, err
}

// Capped is a client's most bitrate within a limit, either 0 for none.
func Capped(kbps, limit int) int {
	if limit == 0 || kbps > 0 && kbps < limit {
		return kbps
	}
	return limit
}
