package provider

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/kv"
)

// MaxAnswer bounds what a provider may answer to one request rather than holding whatever it sends
// in memory; a long show described whole is the largest.
const MaxAnswer = 8 << 20

// builtinHTTP is the built-in providers' HTTP client.
var builtinHTTP = &http.Client{Timeout: 30 * time.Second}

// Client asks a provider for JSON, as every provider is asked: after its share of Limit where
// Limits is set, again after a 429 as the provider says (send), and reading at most MaxAnswer. A
// request goes to Base and nowhere else: what a caller names is a path under it. An error names
// Name and the path, never the address, which may carry a key.
type Client struct {
	Name string
	// Base is the provider's http or https address: a built-in provider's own, or the one an
	// admin registered for a plugin or a server to import from.
	Base string
	// HTTP is nil for a built-in provider's.
	HTTP   *http.Client
	Limits kv.Limiter
	Limit  kv.Limit
}

// Request is what a provider is asked: a path under its Client's Base, already escaped, and what
// goes with it. Body, where it is not nil, is sent as a form where it is url.Values, as OAuth
// requests are (RFC 6749), and as JSON otherwise.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   any
}

// Refusal is a provider's answer other than a 2xx: its status, and its body for what it says.
type Refusal struct {
	Code   int
	Status string
	Body   []byte
}

func (r *Refusal) Error() string { return r.Status }

func (r *Refusal) Is(target error) bool {
	return target == ErrNotFound && r.Code == http.StatusNotFound
}

// Do sends r and decodes a 2xx's JSON into out; any other status is a *Refusal.
func (c Client) Do(ctx context.Context, r Request, out any) error {
	r.Header = r.Header.Clone()
	if r.Header == nil {
		r.Header = http.Header{}
	}
	r.Header.Set("Accept", "application/json")
	data, err := c.Bytes(ctx, r)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s %s: %w", c.Name, r.Path, err)
	}
	return nil
}

// Bytes sends r and answers a 2xx's body as it is, as a write is answered 201 Created; any other
// status is a *Refusal.
func (c Client) Bytes(ctx context.Context, r Request) ([]byte, error) {
	req, err := c.request(ctx, r)
	if err != nil {
		return nil, err
	}
	if c.Limits != nil {
		if err := kv.Wait(ctx, c.Limits, c.Name, c.Limit); err != nil {
			return nil, err
		}
	}
	resp, err := send(cmp.Or(c.HTTP, builtinHTTP), req)
	if err != nil {
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return nil, fmt.Errorf("%s %s: %w: %w", c.Name, r.Path, ErrUnreached, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxAnswer+1))
	switch {
	case err != nil:
		return nil, fmt.Errorf("%s %s: %w: %w", c.Name, r.Path, ErrUnreached, err)
	case len(data) > MaxAnswer:
		return nil, fmt.Errorf("%s %s: answered more than %d bytes", c.Name, r.Path, MaxAnswer)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, fmt.Errorf("%s %s: %w", c.Name, r.Path, &Refusal{Code: resp.StatusCode, Status: resp.Status, Body: data})
	}
	return data, nil
}

// request makes r an HTTP request to Base, refusing a Base that is not an http or https address
// of a host, which no request is sent to.
func (c Client) request(ctx context.Context, r Request) (*http.Request, error) {
	base, err := url.Parse(c.Base)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.Name, err)
	}
	if base.Scheme != "http" && base.Scheme != "https" || base.Host == "" || base.User != nil {
		return nil, fmt.Errorf("%s: %w: its address is not http or https of a host", c.Name, ErrNotConfigured)
	}
	target := base.JoinPath(r.Path)
	target.RawQuery = r.Query.Encode()
	var body io.Reader
	var contentType string
	switch b := r.Body.(type) {
	case nil:
	case url.Values:
		body, contentType = strings.NewReader(b.Encode()), "application/x-www-form-urlencoded"
	default:
		data, err := json.Marshal(b)
		if err != nil {
			return nil, err
		}
		body, contentType = bytes.NewReader(data), "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target.String(), body)
	if err != nil {
		return nil, err
	}
	if r.Header != nil {
		req.Header = r.Header.Clone()
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}

// maxRetryAfter is the longest a provider's asking to be given time is waited out within one
// request; one asking longer fails it, and its job is tried again later.
const maxRetryAfter = 30 * time.Second

// send sends req, and sends it again once a provider answering 429 Too Many Requests says it may,
// so a burst of identifying waits a moment rather than failing titles into their backoff.
func send(hc *http.Client, req *http.Request) (*http.Response, error) {
	var waited time.Duration
	for {
		resp, err := hc.Do(req)
		if err != nil || resp.StatusCode != http.StatusTooManyRequests {
			return resp, err
		}
		wait := retryAfter(resp.Header.Get("Retry-After"))
		if waited += wait; waited > maxRetryAfter {
			return resp, nil
		}
		if err := resp.Body.Close(); err != nil {
			return nil, err
		}
		if req.GetBody != nil {
			if req.Body, err = req.GetBody(); err != nil {
				return nil, err
			}
		}
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(wait):
		}
	}
}

// retryAfter reads a Retry-After header, seconds or a date; a provider that says nothing is given
// a second.
func retryAfter(v string) time.Duration {
	if s, err := strconv.Atoi(v); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0)
	}
	return time.Second
}
