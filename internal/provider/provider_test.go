package provider

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
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
