package domain

import "errors"

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

// Network is how the server is reached, as Plex's Network settings: whether over HTTPS, and the
// certificate it serves, a PEM chain and its key at paths each node reads.
type Network struct {
	Secure      SecureConnections
	Certificate string
	Key         string
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
