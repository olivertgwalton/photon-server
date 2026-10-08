package websocket_test

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/websocket"
	"github.com/olivertgwalton/photon-server/internal/websocket/websockettest"
)

// echo answers each message with itself until the client goes.
func echo(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r)
	if err != nil {
		return
	}
	for {
		m, err := c.Read(time.Minute)
		if err != nil {
			return
		}
		if c.Write(m) != nil {
			return
		}
	}
}

func dial(t *testing.T, srv *httptest.Server) *websockettest.Client {
	t.Helper()
	c, resp, err := websockettest.Dial(t.Context(), srv.URL, http.Header{"Origin": {"https://elsewhere.example"}})
	if err != nil || c == nil {
		t.Fatalf("handshake: %v %v", resp, err)
	}
	resp.Body.Close()
	t.Cleanup(func() { c.Close() })
	return c
}

func next(t *testing.T, c *websockettest.Client) (byte, []byte) {
	t.Helper()
	head, payload, err := c.Next()
	if err != nil {
		t.Fatal(err)
	}
	return head, payload
}

// A client from any origin sends a message in fragments with a ping between them: the ping is
// answered at once and the message read whole. Its close is answered with its own status.
func TestAClientsMessagesAreReadWholeAndItsPingsAnswered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(echo))
	defer srv.Close()
	c := dial(t, srv)
	for _, f := range []struct {
		head    byte
		payload string
	}{
		{websockettest.OpText, `{"MessageType":`},
		{websockettest.Fin | websockettest.OpPing, "are you there"},
		{websockettest.Fin | websockettest.OpContinuation, `"KeepAlive"}`},
	} {
		if err := c.Send(f.head, []byte(f.payload)); err != nil {
			t.Fatal(err)
		}
	}
	if head, payload := next(t, c); head != websockettest.Fin|websockettest.OpPong || string(payload) != "are you there" {
		t.Errorf("the ping answered with %#x %q", head, payload)
	}
	if head, payload := next(t, c); head != websockettest.Fin|websockettest.OpText || string(payload) != `{"MessageType":"KeepAlive"}` {
		t.Errorf("the message echoed as %#x %q", head, payload)
	}
	long := bytes.Repeat([]byte("x"), 70_000)
	if err := c.Send(websockettest.Fin|websockettest.OpText, long[:websocket.MaxMessage]); err != nil {
		t.Fatal(err)
	}
	if _, payload := next(t, c); len(payload) != websocket.MaxMessage {
		t.Errorf("a message of the most a client may send echoed as %d bytes", len(payload))
	}
	if err := c.Send(websockettest.Fin|websockettest.OpClose, binary.BigEndian.AppendUint16(nil, 1000)); err != nil {
		t.Fatal(err)
	}
	if head, payload := next(t, c); head != websockettest.Fin|websockettest.OpClose || binary.BigEndian.Uint16(payload) != 1000 {
		t.Errorf("the close answered with %#x %v", head, payload)
	}
}

// A client that sends too much, or breaks the protocol, is told why and closed.
func TestAClientBreakingTheProtocolIsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(echo))
	defer srv.Close()
	for _, tc := range []struct {
		name  string
		send  func(c *websockettest.Client) error
		close websocket.Status
	}{
		{"too much", func(c *websockettest.Client) error {
			if err := c.Send(websockettest.OpText, bytes.Repeat([]byte("x"), websocket.MaxMessage)); err != nil {
				return err
			}
			return c.Send(websockettest.Fin|websockettest.OpContinuation, []byte("x"))
		}, websocket.StatusTooBig},
		{"unmasked", func(c *websockettest.Client) error {
			_, err := c.Write([]byte{websockettest.Fin | websockettest.OpText, 1, 'x'})
			return err
		}, websocket.StatusProtocolError},
		{"a continuation of nothing", func(c *websockettest.Client) error {
			return c.Send(websockettest.Fin|websockettest.OpContinuation, []byte("x"))
		}, websocket.StatusProtocolError},
		{"text that is not UTF-8", func(c *websockettest.Client) error {
			return c.Send(websockettest.Fin|websockettest.OpText, []byte{0xff})
		}, websocket.StatusInvalidData},
	} {
		c := dial(t, srv)
		if err := tc.send(c); err != nil {
			t.Fatal(err)
		}
		head, payload := next(t, c)
		if head != websockettest.Fin|websockettest.OpClose || len(payload) != 2 || websocket.Status(binary.BigEndian.Uint16(payload)) != tc.close {
			t.Errorf("%s: closed with %#x %v, want %d", tc.name, head, payload, tc.close)
		}
		if _, _, err := c.Next(); err == nil {
			t.Errorf("%s: the connection is still open", tc.name)
		}
	}
}

// A request that does not ask for a WebSocket is refused, and one for another version is told
// which the server speaks.
func TestARequestThatIsNotAHandshakeIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(echo))
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a plain GET: %d, want 400", resp.StatusCode)
	}
	r, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	r.Header = http.Header{
		"Upgrade": {"websocket"}, "Connection": {"keep-alive, Upgrade"},
		"Sec-Websocket-Key": {"dGhlIHNhbXBsZSBub25jZQ=="}, "Sec-Websocket-Version": {"8"},
	}
	resp, err = srv.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUpgradeRequired || resp.Header.Get("Sec-WebSocket-Version") != "13" {
		t.Errorf("version 8: %d %q, want 426 naming 13", resp.StatusCode, resp.Header.Get("Sec-WebSocket-Version"))
	}
}

// wire is a client's connection as the server sees it: what the client sent, then nothing. What
// the server writes is dropped, or kept where sent is.
type wire struct {
	net.Conn
	r    io.Reader
	sent *bytes.Buffer
}

func (w wire) Read(b []byte) (int, error) { return w.r.Read(b) }

func (w wire) Write(b []byte) (int, error) {
	if w.sent != nil {
		return w.sent.Write(b)
	}
	return len(b), nil
}

func (wire) Close() error                     { return nil }
func (wire) SetDeadline(time.Time) error      { return nil }
func (wire) SetReadDeadline(time.Time) error  { return nil }
func (wire) SetWriteDeadline(time.Time) error { return nil }

// hijacked hands the server conn as a request's connection.
type hijacked struct {
	http.ResponseWriter
	conn net.Conn
}

func (h hijacked) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return h.conn, bufio.NewReadWriter(bufio.NewReader(h.conn), bufio.NewWriter(h.conn)), nil
}

// Whatever a client sends once its handshake is accepted ends in messages no larger than
// MaxMessage and then a failure, and the server holds no more than the client sent and a message.
func FuzzRead(f *testing.F) {
	frames := func(fs ...func(c *websockettest.Client) error) []byte {
		var b bytes.Buffer
		c := &websockettest.Client{Conn: wire{sent: &b}}
		for _, send := range fs {
			if err := send(c); err != nil {
				f.Fatal(err)
			}
		}
		return b.Bytes()
	}
	send := func(head byte, payload string) func(c *websockettest.Client) error {
		return func(c *websockettest.Client) error { return c.Send(head, []byte(payload)) }
	}
	f.Add(frames(send(websockettest.Fin|websockettest.OpText, `{"MessageType":"KeepAlive"}`)))
	f.Add(frames(send(websockettest.OpText, `{"MessageType":`), send(websockettest.Fin|websockettest.OpPing, "there?"),
		send(websockettest.Fin|websockettest.OpContinuation, `"KeepAlive"}`)))
	f.Add(frames(send(websockettest.Fin|websockettest.OpPing, "there?")))
	f.Add(frames(send(websockettest.Fin|websockettest.OpClose, "\x03\xe8")))
	f.Add(frames(send(websockettest.Fin|websockettest.OpText, strings.Repeat("x", 200))))
	f.Add(binary.BigEndian.AppendUint64([]byte{websockettest.Fin | websockettest.OpText, 0x80 | 127}, 1<<63))
	f.Fuzz(func(t *testing.T, sent []byte) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header = http.Header{
			"Upgrade": {"websocket"}, "Connection": {"Upgrade"},
			"Sec-Websocket-Key": {"dGhlIHNhbXBsZSBub25jZQ=="}, "Sec-Websocket-Version": {"13"},
		}
		c, err := websocket.Accept(hijacked{httptest.NewRecorder(), wire{r: bytes.NewReader(sent)}}, r)
		if err != nil {
			t.Fatal(err)
		}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		// A message takes six bytes at least, its frame's head and mask, so the last read fails.
		for range len(sent)/6 + 1 {
			m, err := c.Read(time.Minute)
			if err != nil {
				runtime.ReadMemStats(&after)
				if held := after.TotalAlloc - before.TotalAlloc; held > 8*uint64(len(sent))+4*websocket.MaxMessage {
					t.Fatalf("held %d bytes of %d sent", held, len(sent))
				}
				return
			}
			if len(m) > websocket.MaxMessage {
				t.Fatalf("a message of %d bytes", len(m))
			}
		}
		t.Fatalf("more messages read than %d bytes hold", len(sent))
	})
}
