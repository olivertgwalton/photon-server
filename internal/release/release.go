// Package release finds a newer release of the server than the one running, as GitHub lists it.
package release

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

const (
	latestURL  = "https://api.github.com/repos/olivertgwalton/photon-server/releases/latest"
	checkEvery = 6 * time.Hour
	askFor     = 30 * time.Second
	// maxAnswer bounds what is read of GitHub's answer, a release's notes and assets: tens of KiB.
	maxAnswer = 1 << 20
)

// Checker asks GitHub for the latest release while an admin has the check on, and keeps it where
// it is newer than the one running.
type Checker struct {
	running string
	on      func() domain.ReleaseCheck
	log     *slog.Logger
	url     string
	client  *http.Client
	newer   atomic.Pointer[domain.Release]
}

// New checks for releases newer than running, the version this binary was built as, while on says
// to.
func New(running string, on func() domain.ReleaseCheck, log *slog.Logger) *Checker {
	return &Checker{running: running, on: on, log: log, url: latestURL, client: &http.Client{Timeout: askFor}}
}

// Run checks now and every checkEvery until ctx ends. A build of main or a local one is no release,
// and has none to be compared with.
func (c *Checker) Run(ctx context.Context) {
	running, ok := parse(c.running)
	if !ok {
		return
	}
	t := time.NewTicker(checkEvery)
	defer t.Stop()
	for {
		if c.on() == domain.ReleaseCheckOn {
			if err := c.check(ctx, running); err != nil && ctx.Err() == nil {
				c.log.WarnContext(ctx, "no newer release looked for", slog.Any("err", err))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (c *Checker) check(ctx context.Context, running [3]int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "photon-server/"+c.running)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github answered %s", resp.Status)
	}
	var latest struct {
		Tag  string `json:"tag_name"`
		Page string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswer)).Decode(&latest); err != nil {
		return err
	}
	v, ok := parse(latest.Tag)
	if !ok {
		return fmt.Errorf("the latest release's tag %q is no version", latest.Tag)
	}
	if slices.Compare(v[:], running[:]) > 0 {
		c.newer.Store(&domain.Release{Version: latest.Tag, URL: latest.Page})
	} else {
		c.newer.Store(nil)
	}
	return nil
}

// Newer is the latest release where it is newer than the one running and the check is on.
func (c *Checker) Newer() (domain.Release, bool) {
	r := c.newer.Load()
	if r == nil || c.on() != domain.ReleaseCheckOn {
		return domain.Release{}, false
	}
	return *r, true
}

// parse reads a release's tag, vMAJOR.MINOR.PATCH. A pre-release's is not one: none is published
// as the latest.
func parse(tag string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	if !strings.HasPrefix(tag, "v") || len(parts) != len(v) {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}
