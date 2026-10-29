// Package pushrelay asks photon's push relay to wake an Apple or Android device. The relay holds the
// apps' APNs key and Firebase credentials, which no server can be given, and is told nothing of a
// library: a message carries the device's token and the id of a notification, which the device then
// asks its own server for.
package pushrelay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// URL is photon's push relay: fixed, so a device's token is never sent to an address an admin
// typed.
const URL = "https://push.photon.oliverwalton.uk"

// sendTimeout is how long the relay has to hand a message to APNs or FCM.
const sendTimeout = 15 * time.Second

var (
	// ErrGone is a device APNs or FCM says no longer takes notifications: it is to be forgotten.
	ErrGone = errors.New("the device no longer takes notifications")
	// errRefused is a credential the relay does not know, as after its key is changed.
	errRefused = errors.New("the relay refused the server's credential")
)

// Message wakes one device for one notification. Collapse stands for what it is of: APNs and FCM
// keep one of the same for a device that is away, and the device shows one.
type Message struct {
	// Platform is apple or android, which the relay hands to APNs or to FCM.
	Platform domain.PushKind `json:"platform"`
	Token    string          `json:"token"`
	// Environment is the APNs environment that issued an Apple device's token.
	Environment  domain.APNsEnvironment `json:"environment,omitzero"`
	Collapse     string                 `json:"collapse"`
	Notification string                 `json:"notification"`
}

type credentials interface {
	PushRelayCredential(ctx context.Context) (string, error)
	SetPushRelayCredential(ctx context.Context, credential string) error
}

// Relay sends through photon's push relay as one server, asking it for a credential the first time
// and again whenever it no longer knows the one kept.
type Relay struct {
	url    string
	server uuid.UUID
	kept   credentials
	client *http.Client
}

func New(url string, server uuid.UUID, kept credentials) *Relay {
	return &Relay{url: url, server: server, kept: kept, client: &http.Client{Timeout: sendTimeout}}
}

// Send wakes a device. ErrGone for one APNs or FCM no longer delivers to.
func (r *Relay) Send(ctx context.Context, m Message) error {
	credential, err := r.kept.PushRelayCredential(ctx)
	if err != nil {
		return err
	}
	if credential == "" {
		if credential, err = r.register(ctx); err != nil {
			return err
		}
	}
	err = r.send(ctx, credential, m)
	if !errors.Is(err, errRefused) {
		return err
	}
	if credential, err = r.register(ctx); err != nil {
		return err
	}
	return r.send(ctx, credential, m)
}

func (r *Relay) register(ctx context.Context) (string, error) {
	var answer struct {
		Credential string `json:"credential"`
	}
	if err := r.post(ctx, "/v1/servers", "", struct {
		Server uuid.UUID `json:"server"`
	}{r.server}, &answer); err != nil {
		return "", err
	}
	if answer.Credential == "" {
		return "", errors.New("the relay answered no credential")
	}
	return answer.Credential, r.kept.SetPushRelayCredential(ctx, answer.Credential)
}

func (r *Relay) send(ctx context.Context, credential string, m Message) error {
	return r.post(ctx, "/v1/send", credential, m, nil)
}

func (r *Relay) post(ctx context.Context, path, credential string, body, answer any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "photon-server")
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	res, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusGone:
		return ErrGone
	case res.StatusCode == http.StatusUnauthorized:
		return errRefused
	case res.StatusCode/100 != 2:
		return fmt.Errorf("the push relay answered %s", res.Status)
	case answer != nil:
		return json.NewDecoder(res.Body).Decode(answer)
	}
	return nil
}
