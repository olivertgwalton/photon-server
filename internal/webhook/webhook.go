// Package webhook sends events to the addresses that asked for them.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/jobs"
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
		sending, cancel := context.WithTimeout(ctx, deliveryTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(sending, http.MethodPost, d.URL, bytes.NewReader(d.Body))
		if err != nil {
			return err
		}
		mac := hmac.New(sha256.New, []byte(d.Secret))
		mac.Write(d.Body)
		req.Header.Set("Content-Type", "application/json")
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
