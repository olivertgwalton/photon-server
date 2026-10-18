//go:build windows

package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func signed(t *testing.T, key ed25519.PrivateKey, u update) ([]byte, []byte) {
	t.Helper()
	manifest, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	return manifest, ed25519.Sign(key, manifest)
}

func TestOnlyANewerReleaseSignedByPhotonsKeyIsOffered(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, other, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	newer := update{Version: "v1.3.0", URL: "https://example.com/setup.exe", Size: 10, SHA256: "00"}

	manifest, sig := signed(t, key, newer)
	if u, ok, err := verify(manifest, sig, pub, "v1.2.0"); err != nil || !ok || u != newer {
		t.Errorf("a newer release, signed: offered %v, %v, err %v", u, ok, err)
	}
	if _, ok, err := verify(manifest, sig, pub, "(devel)"); err != nil || ok {
		t.Errorf("to a build that is no release: offered %v, err %v", ok, err)
	}
	if _, ok, err := verify(manifest, sig, pub, "v1.3.0"); err != nil || ok {
		t.Errorf("the release running, served again: offered %v, err %v", ok, err)
	}

	manifest, sig = signed(t, other, newer)
	if _, ok, err := verify(manifest, sig, pub, "v1.2.0"); err == nil || ok {
		t.Errorf("signed by another key: offered %v, err %v", ok, err)
	}

	manifest, sig = signed(t, key, newer)
	altered := bytes.Replace(manifest, []byte("v1.3.0"), []byte("v9.9.9"), 1)
	if _, ok, err := verify(altered, sig, pub, "v1.2.0"); err == nil || ok {
		t.Errorf("altered after signing: offered %v, err %v", ok, err)
	}
}

func TestOnlyTheInstallerTheManifestNamesIsKept(t *testing.T) {
	installer := []byte("an installer")
	sum := sha256.Sum256(installer)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write(installer); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	right := update{Version: "v1.3.0", URL: srv.URL + "/setup.exe", Size: int64(len(installer)), SHA256: hex.EncodeToString(sum[:])}

	name, err := download(t.Context(), srv.Client(), right, dir)
	if err != nil {
		t.Fatalf("the installer named: %v", err)
	}
	if got, err := os.ReadFile(name); err != nil || string(got) != string(installer) {
		t.Errorf("the installer kept is %q, err %v", got, err)
	}

	for _, wrong := range []update{
		{Version: right.Version, URL: right.URL, Size: right.Size, SHA256: hex.EncodeToString(make([]byte, sha256.Size))},
		{Version: right.Version, URL: right.URL, Size: right.Size - 1, SHA256: right.SHA256},
	} {
		if _, err := download(t.Context(), srv.Client(), wrong, dir); err == nil {
			t.Errorf("an installer that is not %+v's was kept", wrong)
		}
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Errorf("an installer that is not %+v's is left on disk", wrong)
		}
	}
}
