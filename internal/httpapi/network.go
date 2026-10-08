package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strconv"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/peer"
	"github.com/olivertgwalton/photon-server/internal/secure"
)

type networkSettings interface {
	Network(ctx context.Context) (domain.Network, error)
	SetNetwork(ctx context.Context, n domain.Network) error
}

type secureConnections interface {
	Mode() domain.SecureConnections
}

type jellyfinListener interface {
	Err() error
}

// networkJSON is whether the server's port answers HTTPS, as Plex's Secure connections: required,
// plain HTTP sent to HTTPS but from the server's own machine; preferred, both; disabled, HTTP
// alone, as behind a proxy with the certificate. The certificate is a PEM chain and its key, at
// paths on the server, which required and preferred need. Jellyfin is whether the apps made for
// Jellyfin reach the server too, on a port of its own.
type networkJSON struct {
	SecureConnections domain.SecureConnections `json:"secure_connections"`
	Certificate       string                   `json:"certificate,omitzero"`
	Key               string                   `json:"key,omitzero"`
	Jellyfin          domain.JellyfinMode      `json:"jellyfin"`
	JellyfinPort      int                      `json:"jellyfin_port"`
	// LocalNetworks are the networks whose clients are local, as Jellyfin's LAN networks and
	// Plex's, prefixes or addresses; none is this machine's and the private ones.
	LocalNetworks []string `json:"local_networks"`
	// RemoteMaxBitrateKbps is the most a stream to a client not on them is sent at, as Jellyfin's
	// Internet streaming bitrate limit: its picture's size is the client's still. 0 is no limit.
	RemoteMaxBitrateKbps int `json:"remote_max_bitrate_kbps"`
	// PublicURL is where readers reach the web app from outside, as a television's pairing link
	// names it and secure connections send plain requests to; empty is the address each came to.
	PublicURL string `json:"public_url"`
	// TrustedProxies are the proxies, prefixes or addresses, whose X-Forwarded-For names the
	// client, as Jellyfin's known proxies; none trusts no one.
	TrustedProxies []string `json:"trusted_proxies"`
	// Discovery is whether the server answers apps looking for it on the local network.
	Discovery domain.Discovery `json:"discovery"`
}

func showNetwork(n domain.Network) networkJSON {
	return networkJSON{
		SecureConnections: n.Secure, Certificate: n.Certificate, Key: n.Key,
		Jellyfin: n.Jellyfin, JellyfinPort: n.JellyfinPort, RemoteMaxBitrateKbps: n.RemoteMaxBitrateKbps,
		LocalNetworks: each(n.LocalNetworks, netip.Prefix.String), PublicURL: n.PublicURL,
		TrustedProxies: each(n.TrustedProxies, netip.Prefix.String), Discovery: n.Discovery,
	}
}

// networkStatusJSON is the network as set, and why the node answering is not serving Jellyfin's
// API on its port, where it is not: another program holds it, say.
type networkStatusJSON struct {
	networkJSON
	JellyfinError string `json:"jellyfin_error,omitzero"`
}

func (a *API) adminNetwork(w http.ResponseWriter, r *http.Request) {
	n, err := a.svc.Network.Network(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := networkStatusJSON{networkJSON: showNetwork(n)}
	if err := a.svc.Jellyfin.Err(); err != nil {
		out.JellyfinError = err.Error()
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

// setNetwork replaces how the server is reached, once this node reads the certificate, and tells
// every node, which serves it at once.
func (a *API) setNetwork(w http.ResponseWriter, r *http.Request) {
	var req networkJSON
	if !a.decode(w, r, &req) {
		return
	}
	local, err := peer.Prefixes(req.LocalNetworks)
	if err != nil {
		writeProblem(w, a.logger, codeInvalidBody, "local_networks: "+err.Error())
		return
	}
	proxies, err := peer.Prefixes(req.TrustedProxies)
	if err != nil {
		writeProblem(w, a.logger, codeInvalidBody, "trusted_proxies: "+err.Error())
		return
	}
	public, err := parsePublicURL(req.PublicURL)
	if err != nil {
		writeProblem(w, a.logger, codeInvalidBody, err.Error())
		return
	}
	if req.Discovery == "" {
		writeProblem(w, a.logger, codeInvalidBody, "discovery is broadcast or off")
		return
	}
	n := domain.Network{
		Secure: req.SecureConnections, Certificate: req.Certificate, Key: req.Key,
		Jellyfin: req.Jellyfin, JellyfinPort: req.JellyfinPort, LocalNetworks: local,
		RemoteMaxBitrateKbps: req.RemoteMaxBitrateKbps, TrustedProxies: proxies, Discovery: req.Discovery,
	}
	if public != nil {
		n.PublicURL = public.String()
	}
	if n.RemoteMaxBitrateKbps < 0 {
		writeProblem(w, a.logger, codeInvalidBody, "remote_max_bitrate_kbps is 0, for no limit, or more")
		return
	}
	if _, err := secure.Load(n); err != nil {
		writeProblem(w, a.logger, codeInvalidBody, err.Error())
		return
	}
	_, own, err := net.SplitHostPort(a.svc.Setup.Listen)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if n.JellyfinPort < 1 || n.JellyfinPort > 65535 || own == strconv.Itoa(n.JellyfinPort) {
		writeProblem(w, a.logger, codeInvalidBody, "the Jellyfin port is 1 to 65535, and not the one photon's own API is served on")
		return
	}
	if err := a.svc.Network.SetNetwork(r.Context(), n); err != nil {
		a.internal(w, r, err)
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventNetworkChanged})
	writeJSON(w, a.logger, "application/json", http.StatusOK, showNetwork(n))
}

func (a *API) networkRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/admin/network", access: admin,
			summary: "Whether the server's port answers HTTPS, the certificate it serves, and whether Jellyfin's apps reach it",
			status:  http.StatusOK, reply: networkStatusJSON{}, handle: a.adminNetwork,
		},
		{
			pattern: "PUT /api/v1/admin/network", access: admin,
			summary: "Replace whether the port answers HTTPS, its certificate, and Jellyfin's; every node serves it at once",
			body:    networkJSON{}, status: http.StatusOK, reply: networkJSON{}, handle: a.setNetwork,
		},
	}
}
