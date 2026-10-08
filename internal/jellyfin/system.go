package jellyfin

import (
	"net"
	"net/http"
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
	if a.svc.Proxies.HTTPS(r) {
		scheme = "https"
	}
	return publicInfo{
		LocalAddress: scheme + "://" + r.Host, ServerName: a.name, Version: version, ProductName: product,
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
