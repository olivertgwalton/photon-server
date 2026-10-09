package media

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

var errStalled = errors.New("the server of a .strm's media stopped sending")

// fetchClient fetches a .strm's media. A server that sends no headers within remoteStall is
// given up on; one that stops in its body is given up on by the body (see stallingBody). No limit
// is put on the whole: a film streamed to a player takes as long as it plays.
var fetchClient = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	TLSHandshakeTimeout:   remoteStall,
	ResponseHeaderTimeout: remoteStall,
}}

// Fetch asks for a .strm's media at u, by method with header, as FFmpeg is held to fetching it:
// given up on once its server goes remoteStall without sending.
func Fetch(ctx context.Context, method string, u *url.URL, header http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header = header
	resp, err := fetchClient.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body = stalling(resp.Body, remoteStall)
	return resp, nil
}

// stallingBody ends a body whose server stops sending for after. Only the time a read waits on
// the server counts: a player that stops taking what it is sent, paused, does not end it.
type stallingBody struct {
	io.ReadCloser
	stall   *time.Timer
	after   time.Duration
	stalled chan struct{}
}

func stalling(body io.ReadCloser, after time.Duration) *stallingBody {
	b := &stallingBody{ReadCloser: body, after: after, stalled: make(chan struct{})}
	b.stall = time.AfterFunc(after, func() {
		close(b.stalled)
		b.ReadCloser.Close()
	})
	b.stall.Stop()
	return b
}

func (b *stallingBody) Read(p []byte) (int, error) {
	b.stall.Reset(b.after)
	n, err := b.ReadCloser.Read(p)
	b.stall.Stop()
	select {
	case <-b.stalled:
		return n, errStalled
	default:
		return n, err
	}
}

func (b *stallingBody) Close() error {
	b.stall.Stop()
	return b.ReadCloser.Close()
}
