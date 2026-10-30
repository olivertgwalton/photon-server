package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A write a provider answers 201 Created has worked; a redirect it answers has not.
func TestEvery2xxIsAnAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/created":
			w.WriteHeader(http.StatusCreated)
			if _, err := w.Write([]byte(`{"action":"start"}`)); err != nil {
				t.Error(err)
			}
		case "/moved":
			w.WriteHeader(http.StatusMultipleChoices)
		}
	}))
	defer srv.Close()
	c := Client{Name: "test", Base: srv.URL}
	var body struct{ Action string }
	if err := c.Do(t.Context(), Request{Method: http.MethodPost, Path: "/created"}, &body); err != nil || body.Action != "start" {
		t.Errorf("201: %+v, %v; want it read", body, err)
	}
	_, err := c.Bytes(t.Context(), Request{Method: http.MethodGet, Path: "/moved"})
	if refusal, ok := errors.AsType[*Refusal](err); !ok || refusal.Code != http.StatusMultipleChoices {
		t.Errorf("300: %v, want a refusal", err)
	}
}

// An OAuth endpoint that takes only a form is sent one, at its path as written: MDBList's refuse
// a path without its trailing slash.
func TestAFormBodyIsSentAsAForm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token/" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" ||
			r.FormValue("grant_type") != "refresh_token" {
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	c := Client{Name: "test", Base: srv.URL}
	form := url.Values{"grant_type": {"refresh_token"}}
	if _, err := c.Bytes(t.Context(), Request{Method: http.MethodPost, Path: "/oauth/token/", Body: form}); err != nil {
		t.Error(err)
	}
}

// subtitler finds a subtitle for any title, where it answers subtitles at all.
type subtitler struct {
	id      domain.FieldSource
	answers bool
}

func (s subtitler) Info() Info                                            { return Info{ID: s.id} }
func (s subtitler) Answers(c domain.Capability) bool                      { return s.answers }
func (s subtitler) FetchSubtitle(context.Context, string) ([]byte, error) { return []byte("1"), nil }

func (s subtitler) SearchSubtitles(context.Context, domain.SubtitleQuery) ([]domain.FoundSubtitle, error) {
	return []domain.FoundSubtitle{{Source: s.id, ID: "1"}}, nil
}

// A plugin whose manifest does not name subtitles is not asked for them, though it has the methods.
func TestOnlyAProviderThatAnswersSubtitlesIsAskedForThem(t *testing.T) {
	silent := subtitler{id: "plugin:silent"}
	r := NewRegistry(nil, silent, subtitler{id: "plugin:subs", answers: true})
	found, err := r.SearchSubtitles(t.Context(), domain.SubtitleQuery{})
	if err != nil || len(found) != 1 || found[0].Source != "plugin:subs" {
		t.Errorf("found = %+v %v, want the answering plugin's alone", found, err)
	}
	if _, err := r.FetchSubtitle(t.Context(), silent.id, "1"); !errors.Is(err, ErrNoSubtitler) {
		t.Errorf("fetching from the silent plugin: %v, want ErrNoSubtitler", err)
	}
}

// pauses is Valkey's pauses held in memory, shared by every client given it as nodes share Valkey.
type pauses map[string]time.Time

func (p pauses) Pause(_ context.Context, name string, d time.Duration) error {
	if until := time.Now().Add(d); until.After(p[name]) {
		p[name] = until
	}
	return nil
}

func (p pauses) Paused(_ context.Context, name string) (time.Duration, error) {
	return max(time.Until(p[name]), 0), nil
}

// A provider asking to be left longer than a request waits, as MDBList does once a day's requests
// are spent, is not asked again by any node until then.
func TestALongPauseHoldsEveryNodeBack(t *testing.T) {
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked++
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	shared := pauses{}
	one, other := Client{Name: "test", Base: srv.URL, Pauses: shared}, Client{Name: "test", Base: srv.URL, Pauses: shared}
	if _, err := one.Bytes(t.Context(), Request{Method: http.MethodGet, Path: "/"}); !errors.Is(err, ErrLimited) {
		t.Errorf("asked to wait an hour: %v, want ErrLimited", err)
	}
	if _, err := other.Bytes(t.Context(), Request{Method: http.MethodGet, Path: "/"}); !errors.Is(err, ErrLimited) || asked != 1 {
		t.Errorf("another node within the hour: %v after %d requests; want ErrLimited, the provider asked once", err, asked)
	}
	unrelated := Client{Name: "another provider", Base: srv.URL, Pauses: shared}
	if _, err := unrelated.Bytes(t.Context(), Request{Method: http.MethodGet, Path: "/"}); asked != 2 {
		t.Errorf("another provider: %v after %d requests; want it asked", err, asked)
	}
}

// A moment another node was asked to wait is waited out before asking, not spent on a 429.
func TestAShortPauseIsWaitedOut(t *testing.T) {
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked++
		if _, err := w.Write([]byte(`{}`)); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	shared := pauses{}
	if err := shared.Pause(t.Context(), "test", 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	c := Client{Name: "test", Base: srv.URL, Pauses: shared}
	if _, err := c.Bytes(t.Context(), Request{Method: http.MethodGet, Path: "/"}); err != nil || asked != 1 {
		t.Fatalf("after the pause: %v after %d requests; want an answer", err, asked)
	}
	if waited := time.Since(start); waited < 150*time.Millisecond {
		t.Errorf("asked after %v, want the pause waited out", waited)
	}
}
