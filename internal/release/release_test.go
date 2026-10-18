package release

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type answer func(*http.Request) (*http.Response, error)

func (a answer) RoundTrip(r *http.Request) (*http.Response, error) { return a(r) }

// checker checks against a GitHub that answers latest as the latest release, and counts in asked
// what it is asked.
func checker(running, latest string, check *domain.ReleaseCheck, asked *int) *Checker {
	c := New(running, func() domain.ReleaseCheck { return *check }, slog.New(slog.DiscardHandler))
	c.client.Transport = answer(func(*http.Request) (*http.Response, error) {
		*asked++
		body := `{"tag_name":"` + latest + `","html_url":"https://github.com/o/p/releases/tag/` + latest + `"}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	return c
}

// running runs c until it has checked twice, at start and checkEvery later, and answers what it
// found.
func running(t *testing.T, c *Checker) (domain.Release, bool) {
	t.Helper()
	var r domain.Release
	var ok bool
	synctest.Test(t, func(t *testing.T) {
		ctx, stop := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			c.Run(ctx)
		}()
		synctest.Wait()
		<-time.After(checkEvery)
		synctest.Wait()
		r, ok = c.Newer()
		stop()
		<-done
	})
	return r, ok
}

func TestAnAdminIsToldOfANewerReleaseWhileTheCheckIsOn(t *testing.T) {
	check, asked := domain.ReleaseCheckOn, 0
	c := checker("v0.9.3", "v0.10.0", &check, &asked)
	got, ok := running(t, c)
	want := domain.Release{Version: "v0.10.0", URL: "https://github.com/o/p/releases/tag/v0.10.0"}
	if !ok || got != want || asked != 2 {
		t.Errorf("newer: %+v, %v, asked %d times; want %+v, asked twice", got, ok, asked, want)
	}
	check = domain.ReleaseCheckOff
	if got, ok := c.Newer(); ok {
		t.Errorf("with the check off: %+v, want none", got)
	}
}

func TestTheReleaseRunningIsNotNewer(t *testing.T) {
	for _, v := range []string{"v0.10.0", "v0.10.1", "v1.0.0"} {
		check, asked := domain.ReleaseCheckOn, 0
		if got, ok := running(t, checker(v, "v0.10.0", &check, &asked)); ok {
			t.Errorf("running %s: %+v is newer", v, got)
		}
	}
}

// GitHub is asked nothing with the check off, nor by a build that is no release.
func TestGitHubIsAskedOnlyByAReleaseWithTheCheckOn(t *testing.T) {
	for _, tc := range []struct {
		running string
		check   domain.ReleaseCheck
	}{
		{"v0.9.3", domain.ReleaseCheckOff},
		{"(devel)", domain.ReleaseCheckOn},
		{"170fa25d0c6e1b6e4f6f3c1d9a4e8b2f7c5d1a3e", domain.ReleaseCheckOn},
		{"v0.10.0-rc.1", domain.ReleaseCheckOn},
	} {
		asked := 0
		running(t, checker(tc.running, "v0.10.0", &tc.check, &asked))
		if asked != 0 {
			t.Errorf("%s with the check %s asked %d times, want none", tc.running, tc.check, asked)
		}
	}
}
