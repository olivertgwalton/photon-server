//go:build windows

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/release"
)

// version is the release the tray was built as, set at build time; a build of main or a local
// one is no release, and is offered none.
var version = "(devel)"

const (
	// manifestURL is the manifest each release publishes beside its installer, with its signature
	// at manifestURL + ".sig".
	manifestURL = "https://github.com/olivertgwalton/photon-server/releases/latest/download/photon-server_windows_amd64_update.json"
	// checkEvery is how often the tray looks for an update, as often as Sparkle does on the Mac.
	checkEvery = 24 * time.Hour
	askFor     = 30 * time.Second
	// maxManifest bounds what is read of a manifest or its signature: a few hundred bytes.
	maxManifest = 64 << 10
)

// updateKey is the public half of the Ed25519 key release.yml signs each manifest with: Sparkle's,
// so one key says that an update of either app is Photon's.
//
//go:embed update-key.pub
var updateKey string

// update is what a manifest offers: a release's installer, which is refused unless its size and
// SHA-256 are these.
type update struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

// check fetches the latest manifest and its signature, and answers the update it offers, and
// whether it is one: a release newer than running.
func check(ctx context.Context, client *http.Client, running string) (update, bool, error) {
	manifest, err := fetch(ctx, client, manifestURL)
	if err != nil {
		return update{}, false, err
	}
	sig, err := fetch(ctx, client, manifestURL+".sig")
	if err != nil {
		return update{}, false, err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(updateKey))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return update{}, false, errors.New("the tray's update key is no Ed25519 key")
	}
	return verify(manifest, sig, key, running)
}

// verify reads a manifest key signed. The version is in what is signed, so an older release's
// manifest, served again, offers nothing.
func verify(manifest, sig []byte, key ed25519.PublicKey, running string) (update, bool, error) {
	if !ed25519.Verify(key, manifest, sig) {
		return update{}, false, errors.New("the update was not signed by Photon's key")
	}
	var u update
	if err := json.Unmarshal(manifest, &u); err != nil {
		return update{}, false, err
	}
	if !release.Newer(u.Version, running) {
		return u, false, nil
	}
	if where, err := url.Parse(u.URL); err != nil || where.Scheme != "https" || u.Size <= 0 {
		return update{}, false, fmt.Errorf("the update's manifest names no installer: %q", u.URL)
	}
	return u, true, nil
}

func fetch(ctx context.Context, client *http.Client, from string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, askFor)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, from, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Photon/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", from, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxManifest))
}

// download writes u's installer into dir, answering its path once its size and SHA-256 are the
// manifest's; one that is not is removed.
func download(ctx context.Context, client *http.Client, u update, dir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Photon/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the installer's download answered %s", resp.Status)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := filepath.Join(dir, path.Base(req.URL.Path))
	f, err := os.Create(name)
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), io.LimitReader(resp.Body, u.Size+1))
	err = errors.Join(err, f.Close())
	switch {
	case err != nil:
	case n != u.Size:
		err = fmt.Errorf("the installer is %d bytes, not the %d its manifest says", n, u.Size)
	case hex.EncodeToString(sum.Sum(nil)) != u.SHA256:
		err = errors.New("the installer is not the one its manifest names")
	}
	if err != nil {
		return "", errors.Join(err, os.Remove(name))
	}
	return name, nil
}
