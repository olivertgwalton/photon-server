package domain

import (
	"errors"
	"net/netip"
)

// SecureConnections is whether the server's port answers HTTPS, as Plex's setting of that name:
// required, plain HTTP is sent to HTTPS but from this machine; preferred, both are answered;
// disabled, HTTP alone, as behind a proxy that has the certificate.
type SecureConnections string

const (
	SecureRequired  SecureConnections = "required"
	SecurePreferred SecureConnections = "preferred"
	SecureDisabled  SecureConnections = "disabled"
)

func SecureConnectionModes() []SecureConnections {
	return []SecureConnections{SecureRequired, SecurePreferred, SecureDisabled}
}

// JellyfinMode is whether the server also answers Jellyfin's API, on a port of its own, for the
// apps made for Jellyfin.
type JellyfinMode string

const (
	JellyfinOn  JellyfinMode = "on"
	JellyfinOff JellyfinMode = "off"
)

func JellyfinModes() []JellyfinMode {
	return []JellyfinMode{JellyfinOn, JellyfinOff}
}

// Network is how the server is reached, as Plex's Network settings: whether over HTTPS, and the
// certificate it serves, a PEM chain and its key at paths each node reads; whether Jellyfin's apps
// reach it too, and on which port; the networks whose clients are local, none for the private
// ones; and the most a stream to a client outside them is sent at, 0 for no limit.
type Network struct {
	Secure               SecureConnections
	Certificate          string
	Key                  string
	Jellyfin             JellyfinMode
	JellyfinPort         int
	LocalNetworks        []netip.Prefix
	RemoteMaxBitrateKbps int
}

var ErrNoCertificate = errors.New("secure connections need a certificate and its key")

func (n Network) Check() error {
	switch n.Secure {
	case SecureRequired, SecurePreferred:
		if n.Certificate == "" || n.Key == "" {
			return ErrNoCertificate
		}
	case SecureDisabled:
	}
	return nil
}
