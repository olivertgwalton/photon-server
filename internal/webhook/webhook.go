// Package webhook sends events to the addresses that asked for them.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/events"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/plugin/pluginv1"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// deliveryTimeout is how long a webhook has to answer; one that does not is tried again later.
const deliveryTimeout = 10 * time.Second

// client follows no redirect: one would turn the POST into a GET with no body.
var client = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// Deliver is the job that sends one delivery. Anything but a 2xx answer fails it, and the queue
// tries it again later.
func Deliver(st *store.Store) jobs.Handler {
	return func(ctx context.Context, id uuid.UUID) error {
		d, err := st.Delivery(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.Plugin != nil {
			// A plugin is sent its settings as they are now, which a delivery never keeps.
			set, err := st.ProviderSettings(ctx, domain.PluginSource(*d.Plugin))
			if err != nil {
				return err
			}
			if d.Body, err = json.Marshal(pluginv1.EventRequest{Settings: set, Event: d.Body}); err != nil {
				return err
			}
		}
		header, body, err := shape(d.Format, d.Body)
		if err != nil {
			return err
		}
		sending, cancel := context.WithTimeout(ctx, deliveryTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(sending, http.MethodPost, d.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header = header
		mac := hmac.New(sha256.New, []byte(d.Secret))
		mac.Write(body)
		req.Header.Set("User-Agent", "photon-server")
		req.Header.Set("X-Photon-Event", string(d.Kind))
		req.Header.Set("X-Photon-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		res, err := client.Do(req)
		if err != nil {
			// The address may carry a token of the receiver's, so the error names only its host.
			if ue, ok := errors.AsType[*url.Error](err); ok {
				err = ue.Err
			}
			return fmt.Errorf("%s: %w", req.URL.Host, err)
		}
		res.Body.Close()
		if res.StatusCode/100 != 2 {
			return fmt.Errorf("%s answered %s", req.URL.Host, res.Status)
		}
		return st.Delivered(ctx, id)
	}
}

// shape is a body as its webhook's format sends it, and the headers it is sent with: photon's own
// as it is, or the server's name over what happened, as a chat or push service takes a message.
func shape(f domain.WebhookFormat, body []byte) (http.Header, []byte, error) {
	header := http.Header{"Content-Type": {"application/json"}}
	var told struct {
		Server events.Server `json:"server"`
		Text   string        `json:"text"`
	}
	if f != domain.WebhookPhoton {
		if err := json.Unmarshal(body, &told); err != nil {
			return nil, nil, err
		}
	}
	var message any
	switch f {
	case domain.WebhookPhoton:
		return header, body, nil
	case domain.WebhookDiscord:
		message = struct {
			Username string `json:"username"`
			Content  string `json:"content"`
		}{told.Server.Name, told.Text}
	case domain.WebhookSlack:
		message = struct {
			Text string `json:"text"`
		}{told.Server.Name + ": " + told.Text}
	case domain.WebhookNtfy:
		header.Set("Content-Type", "text/plain; charset=utf-8")
		// A header is ASCII; ntfy reads a name that is not as RFC 2047 words.
		header.Set("Title", mime.QEncoding.Encode("utf-8", told.Server.Name))
		return header, []byte(told.Text), nil
	case domain.WebhookGotify:
		message = struct {
			Title   string `json:"title"`
			Message string `json:"message"`
		}{told.Server.Name, told.Text}
	}
	out, err := json.Marshal(message)
	return header, out, err
}
