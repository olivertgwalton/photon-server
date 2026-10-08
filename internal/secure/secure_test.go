package secure

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// selfSigned writes a certificate for localhost and its key, answering their paths and a pool
// that trusts it.
func selfSigned(t *testing.T) (cert, key string, pool *x509.CertPool) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{"localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cert, key = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	for path, block := range map[string]*pem.Block{cert: {Type: "CERTIFICATE", Bytes: der}, key: {Type: "PRIVATE KEY", Bytes: keyDER}} {
		if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool = x509.NewCertPool()
	pool.AddCert(parsed)
	return cert, key, pool
}

type setting struct {
	mu sync.Mutex
	n  domain.Network
}

func (s *setting) Network(context.Context) (domain.Network, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n, nil
}

func (s *setting) set(n domain.Network) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n = n
}

// One port answers HTTPS and plain HTTP while secure connections are preferred, and once they are
// disabled, without a restart, closes HTTPS at once without logging it and still answers HTTP.
func TestOnePortServesWhatIsSet(t *testing.T) {
	cert, key, pool := selfSigned(t)
	st := &setting{n: domain.Network{Secure: domain.SecurePreferred, Certificate: cert, Key: key}}
	s := New(st, nil, slog.New(slog.DiscardHandler))
	s.reread(t.Context())

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var logged lockedBuffer
	srv := &http.Server{ErrorLog: log.New(&logged, "", 0), TLSConfig: s.TLSConfig(), ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.WriteString(w, r.Proto); err != nil {
			t.Error(err)
		}
	})}
	go func() {
		if err := srv.Serve(s.Listen(l)); !errors.Is(err, http.ErrServerClosed) {
			t.Error(err)
		}
	}()
	t.Cleanup(func() { srv.Close() })
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	https := "https://localhost:" + port
	secure := &http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}, ForceAttemptHTTP2: true}}
	get := func(c *http.Client, url string) (string, error) {
		resp, err := c.Get(url)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		return string(body), err
	}

	if proto, err := get(secure, https); err != nil || proto != "HTTP/2.0" {
		t.Errorf("over HTTPS: %q, %v; want HTTP/2.0", proto, err)
	}
	if proto, err := get(http.DefaultClient, "http://localhost:"+port); err != nil || proto != "HTTP/1.1" {
		t.Errorf("over plain HTTP: %q, %v; want HTTP/1.1", proto, err)
	}

	st.set(domain.Network{Secure: domain.SecureDisabled})
	s.reread(t.Context())
	secure.CloseIdleConnections()
	if _, err := get(secure, https); err == nil {
		t.Error("HTTPS is still answered once disabled")
	}
	if proto, err := get(http.DefaultClient, "http://localhost:"+port); err != nil || proto != "HTTP/1.1" {
		t.Errorf("over plain HTTP once disabled: %q, %v; want HTTP/1.1", proto, err)
	}
	if got := logged.String(); got != "" {
		t.Errorf("logged %q, want nothing", got)
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Every node takes up an admin's change as it is told of it, and a certificate it cannot read
// leaves it serving as it was.
func TestANodeServesAChangeAsItIsTold(t *testing.T) {
	cert, key, _ := selfSigned(t)
	synctest.Test(t, func(t *testing.T) {
		st := &setting{n: domain.Network{Secure: domain.SecureDisabled}}
		events := make(chan domain.Event)
		s := New(st, func() (<-chan domain.Event, func()) { return events, func() {} }, slog.New(slog.DiscardHandler))
		ctx, stop := context.WithCancel(t.Context())
		defer stop()
		go s.Run(ctx)

		st.set(domain.Network{Secure: domain.SecureRequired, Certificate: cert, Key: key})
		events <- domain.Event{Kind: domain.EventNetworkChanged}
		synctest.Wait()
		if got := s.Mode(); got != domain.SecureRequired {
			t.Errorf("told of the change: %s, want required", got)
		}

		st.set(domain.Network{Secure: domain.SecurePreferred, Certificate: "/gone.pem", Key: "/gone.key"})
		events <- domain.Event{Kind: domain.EventNetworkChanged}
		synctest.Wait()
		if got := s.Mode(); got != domain.SecureRequired {
			t.Errorf("an unreadable certificate: %s, want required as it was", got)
		}
	})
}
