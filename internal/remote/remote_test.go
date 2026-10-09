package remote

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// provider offers whatever it is set to, once let, counting how often it is asked.
type provider struct {
	asked  atomic.Int32
	offers []domain.Offer
	let    chan struct{}
}

func (p *provider) Streams(context.Context, domain.FieldSource, domain.Streamed) ([]domain.Offer, error) {
	p.asked.Add(1)
	if p.let != nil {
		<-p.let
	}
	return p.offers, nil
}

var (
	aio  = domain.PluginSource("aio")
	heat = domain.Streamed{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0113277"}}
)

// A play's requests, many at once and again within the minute, ask the provider once; a minute
// on, it is asked again, as a link it gave may have expired.
func TestAProviderIsAskedOnceAMinute(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := &provider{let: make(chan struct{})}
		o := New(p)
		var wg sync.WaitGroup
		for range 5 {
			wg.Go(func() {
				if _, err := o.Of(t.Context(), aio, heat); err != nil {
					t.Error(err)
				}
			})
		}
		synctest.Wait()
		close(p.let)
		wg.Wait()
		if _, err := o.Of(t.Context(), aio, heat); err != nil || p.asked.Load() != 1 {
			t.Errorf("asked %d times, %v; want once", p.asked.Load(), err)
		}
		<-time.After(offersFor)
		if _, err := o.Of(t.Context(), aio, heat); err != nil || p.asked.Load() != 2 {
			t.Errorf("a minute on, asked %d times, %v; want again", p.asked.Load(), err)
		}
	})
}

// A part is the copy its provider offers now with its fingerprint, opened through the relay; one
// no longer offered is gone.
func TestAPartIsOpenedWhereItsCopyIsOfferedNow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.WriteString(w, "the film "+r.URL.Path); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	at := func(path string) *url.URL {
		u, err := url.Parse(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	from := strings.TrimPrefix(srv.URL, "http://")
	uhd := domain.Offer{Key: "torrent:abc:0", Name: "Heat.2160p.mkv", URL: at("/uhd"), From: from}
	hd := domain.Offer{Key: "torrent:abc:1", Name: "Heat.1080p.mkv", URL: at("/hd"), From: from}
	p := &provider{offers: []domain.Offer{uhd, hd}}
	o := New(p)
	in, err := o.Open(t.Context(), domain.Place{Media: domain.MediaRemote, Rel: Rel(aio, hd), Source: aio, Title: heat})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	resp, err := http.Get(in.URL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if b, err := io.ReadAll(resp.Body); err != nil || string(b) != "the film /hd" || in.Name != "Heat.1080p.mkv" {
		t.Errorf("opened %q named %q, want the 1080p copy", b, in.Name)
	}
	gone := domain.Offer{Key: "file:Heat.mkv:1", Name: "Heat.mkv"}
	if _, err := o.Open(t.Context(), domain.Place{Media: domain.MediaRemote, Rel: Rel(aio, gone), Source: aio, Title: heat}); !errors.Is(err, ErrGone) {
		t.Errorf("a copy no longer offered: %v, want ErrGone", err)
	}
}

func TestACopyIsFiledByItsFingerprintAndName(t *testing.T) {
	rel := Rel(aio, domain.Offer{Key: "k", Name: "../Heat/1995.mkv"})
	known, name, _ := strings.Cut(rel, "/")
	if len(known) != 64 || name != "..-Heat-1995.mkv" {
		t.Errorf("filed as %q", rel)
	}
}
