package jellyfin

import (
	"net"
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/peer"
)

// publicInfo is Jellyfin's PublicSystemInfo, which an app reads first and anyone may.
type publicInfo struct {
	// LocalAddress is where the app reached the server, which is where it can reach it again.
	LocalAddress           string `json:"LocalAddress"`
	ServerName             string `json:"ServerName"`
	Version                string `json:"Version"`
	ProductName            string `json:"ProductName"`
	OperatingSystem        string `json:"OperatingSystem"`
	ID                     string `json:"Id"`
	StartupWizardCompleted bool   `json:"StartupWizardCompleted"`
}

func (a *API) public(r *http.Request) publicInfo {
	scheme := "http"
	if a.svc.Reach.HTTPS(r) {
		scheme = "https"
	}
	return publicInfo{
		LocalAddress: scheme + "://" + r.Host, ServerName: a.name(), Version: version, ProductName: product,
		ID: a.id, StartupWizardCompleted: true,
	}
}

func (a *API) publicInfo(w http.ResponseWriter, r *http.Request) {
	a.writeJSON(w, a.public(r))
}

// systemInfo is Jellyfin's SystemInfo: what an app asks of a server it is signed in to. Jellyfin's
// paths on disk are left out, as obsolete there and nobody's business here.
type systemInfo struct {
	HasPendingRestart        bool       `json:"HasPendingRestart"`
	IsShuttingDown           bool       `json:"IsShuttingDown"`
	SupportsLibraryMonitor   bool       `json:"SupportsLibraryMonitor"`
	WebSocketPortNumber      int        `json:"WebSocketPortNumber"`
	CompletedInstallations   []struct{} `json:"CompletedInstallations"`
	CastReceiverApplications []struct{} `json:"CastReceiverApplications"`
	publicInfo
}

func (a *API) systemInfo(w http.ResponseWriter, r *http.Request) {
	port := 0
	if addr, ok := r.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr); ok {
		port = addr.Port
	}
	a.writeJSON(w, systemInfo{
		SupportsLibraryMonitor: true, WebSocketPortNumber: port,
		CompletedInstallations: []struct{}{}, CastReceiverApplications: []struct{}{},
		publicInfo: a.public(r),
	})
}

// endpointInfo is Jellyfin's EndPointInfo: whether the app is on the server's own machine, and on
// its networks. Jellyfin's web app measures its bitrate only once it knows, and else streams at
// 1.5 Mbps.
type endpointInfo struct {
	IsLocal     bool `json:"IsLocal"`
	IsInNetwork bool `json:"IsInNetwork"`
}

func (a *API) endpoint(w http.ResponseWriter, r *http.Request) {
	n, err := a.svc.Network.Network(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	client := a.svc.Reach.Client(r).Unmap()
	a.writeJSON(w, endpointInfo{IsLocal: client.IsLoopback(), IsInNetwork: peer.LocalIn(n.LocalNetworks, client)})
}
