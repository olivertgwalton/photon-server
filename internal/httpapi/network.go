package httpapi

import (
	"context"
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/secure"
)

type networkSettings interface {
	Network(ctx context.Context) (domain.Network, error)
	SetNetwork(ctx context.Context, n domain.Network) error
}

type secureConnections interface {
	Mode() domain.SecureConnections
}

// networkJSON is whether the server's port answers HTTPS, as Plex's Secure connections: required,
// plain HTTP sent to HTTPS but from the server's own machine; preferred, both; disabled, HTTP
// alone, as behind a proxy with the certificate. The certificate is a PEM chain and its key, at
// paths on the server, which required and preferred need.
type networkJSON struct {
	SecureConnections domain.SecureConnections `json:"secure_connections"`
	Certificate       string                   `json:"certificate,omitzero"`
	Key               string                   `json:"key,omitzero"`
}

func showNetwork(n domain.Network) networkJSON {
	return networkJSON{SecureConnections: n.Secure, Certificate: n.Certificate, Key: n.Key}
}

func (a *API) adminNetwork(w http.ResponseWriter, r *http.Request) {
	n, err := a.svc.Network.Network(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, showNetwork(n))
}

// setNetwork replaces how the server is reached, once this node reads the certificate, and tells
// every node, which serves it at once.
func (a *API) setNetwork(w http.ResponseWriter, r *http.Request) {
	var req networkJSON
	if !a.decode(w, r, &req) {
		return
	}
	n := domain.Network{Secure: req.SecureConnections, Certificate: req.Certificate, Key: req.Key}
	if _, err := secure.Load(n); err != nil {
		writeProblem(w, a.logger, codeInvalidBody, err.Error())
		return
	}
	if err := a.svc.Network.SetNetwork(r.Context(), n); err != nil {
		a.internal(w, r, err)
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventNetworkChanged})
	writeJSON(w, a.logger, "application/json", http.StatusOK, showNetwork(n))
}
