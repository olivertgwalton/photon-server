// Package discovery answers clients on the local network asking where the server is, as
// Jellyfin answers "who is JellyfinServer?" on UDP 7359.
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/httpapi"
)

// Question is what a client broadcasts. It is matched ignoring case and surrounding white space.
const Question = "who is PhotonServer?"

type answer struct {
	httpapi.Info
	// Address is where the client reaches the server's HTTP API.
	Address string `json:"address"`
}

// Serve answers on conn, which is bound to the HTTP listener's port number, until ctx ends.
func Serve(ctx context.Context, conn net.PacketConn, info httpapi.Info, logger *slog.Logger) error {
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	bound, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return errors.New("discovery needs a UDP socket")
	}
	port := strconv.Itoa(bound.Port)
	buf := make([]byte, 512)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		asker, ok := from.(*net.UDPAddr)
		if !ok || !nearby(asker.AddrPort().Addr()) || !strings.EqualFold(strings.TrimSpace(string(buf[:n])), Question) {
			continue
		}
		if err := reply(conn, asker, info, port); err != nil {
			logger.WarnContext(ctx, "discovery not answered", slog.String("asker", asker.String()), slog.Any("err", err))
		}
	}
}

// nearby is whether an asker is on this machine or a private network. A reply is several times
// the question, so answering anyone else would lend the server to an amplification attack.
func nearby(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast()
}

func reply(conn net.PacketConn, asker *net.UDPAddr, info httpapi.Info, port string) error {
	// A connected socket toward the asker is bound to this machine's address on the interface
	// facing it; nothing is sent on it.
	toward, err := net.DialUDP("udp", nil, asker)
	if err != nil {
		return err
	}
	local, ok := toward.LocalAddr().(*net.UDPAddr)
	toward.Close()
	if !ok {
		return errors.New("no local address toward the asker")
	}
	// A link-local zone names this machine's interface, which means nothing to the asker.
	host := local.AddrPort().Addr().Unmap().WithZone("").String()
	body, err := json.Marshal(answer{Info: info, Address: "http://" + net.JoinHostPort(host, port)})
	if err != nil {
		return err
	}
	_, err = conn.WriteTo(body, asker)
	return err
}
