// Package discovery answers clients on the local network asking where the server is, as
// Jellyfin answers "who is JellyfinServer?" on UDP 7359.
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/peer"
)

// Question is what a client broadcasts. It is matched ignoring case and surrounding white space.
const Question = "who is PhotonServer?"

type answer struct {
	domain.Info
	// Address is where the client reaches the server's API, over HTTPS where it serves it.
	Address string `json:"address"`
}

// Serve answers on conn, which is bound to the HTTP listener's port number, until ctx ends. scheme
// is http or https, as the listener serves when asked.
func Serve(ctx context.Context, conn net.PacketConn, info domain.Info, scheme func() string, logger *slog.Logger) error {
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
		// A reply is several times the question, so answering anyone not local would lend the
		// server to an amplification attack.
		if !ok || !peer.Local(asker.AddrPort().Addr()) || !strings.EqualFold(strings.TrimSpace(string(buf[:n])), Question) {
			continue
		}
		if err := reply(conn, asker, info, scheme(), port); err != nil {
			logger.WarnContext(ctx, "discovery not answered", slog.String("asker", asker.String()), slog.Any("err", err))
		}
	}
}

func reply(conn net.PacketConn, asker *net.UDPAddr, info domain.Info, scheme, port string) error {
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
	body, err := json.Marshal(answer{Info: info, Address: scheme + "://" + net.JoinHostPort(host, port)})
	if err != nil {
		return err
	}
	_, err = conn.WriteTo(body, asker)
	return err
}
