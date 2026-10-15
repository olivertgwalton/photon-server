package webhook

import (
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Each format sends what a chat or push service shows: the server's name over what happened, as
// the service takes a message; photon's own is sent as it is.
func TestAnEventIsSentAsItsWebhooksServiceTakesIt(t *testing.T) {
	body := []byte(`{"event":"playback.started","server":{"id":"0198f2a4-6c1e-7000-8000-000000000001","name":"Den Café"},"text":"Ada started Heat"}`)
	for _, c := range []struct {
		format      domain.WebhookFormat
		contentType string
		title       string
		want        string
	}{
		{domain.WebhookPhoton, "application/json", "", string(body)},
		{domain.WebhookDiscord, "application/json", "", `{"username":"Den Café","content":"Ada started Heat"}`},
		{domain.WebhookSlack, "application/json", "", `{"text":"Den Café: Ada started Heat"}`},
		{domain.WebhookNtfy, "text/plain; charset=utf-8", "=?utf-8?q?Den_Caf=C3=A9?=", "Ada started Heat"},
		{domain.WebhookGotify, "application/json", "", `{"title":"Den Café","message":"Ada started Heat"}`},
	} {
		header, got, err := shape(c.format, body)
		if err != nil || string(got) != c.want || header.Get("Content-Type") != c.contentType || header.Get("Title") != c.title {
			t.Errorf("%s: %s %v %v, want %s as %s titled %q", c.format, got, header, err, c.want, c.contentType, c.title)
		}
	}
	if _, _, err := shape(domain.WebhookDiscord, []byte("not json")); err == nil || strings.Contains(err.Error(), "Den") {
		t.Errorf("a body that is not JSON: %v, want an error", err)
	}
}
