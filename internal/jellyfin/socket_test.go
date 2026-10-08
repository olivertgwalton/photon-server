package jellyfin

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/olivertgwalton/photon-server/internal/websocket/websockettest"
)

func openSocket(t *testing.T, target string, header http.Header) *websockettest.Client {
	t.Helper()
	c, resp, err := websockettest.Dial(t.Context(), target, header)
	if err != nil || c == nil {
		t.Fatalf("%s: %v %v", target, resp, err)
	}
	resp.Body.Close()
	t.Cleanup(func() { c.Close() })
	return c
}

func say(t *testing.T, c *websockettest.Client, m string) {
	t.Helper()
	if err := c.Send(websockettest.Fin|websockettest.OpText, []byte(m)); err != nil {
		t.Fatal(err)
	}
}

// heard reads the server's next message, failing on any frame that is not one.
func heard(t *testing.T, c *websockettest.Client) map[string]any {
	t.Helper()
	head, payload, err := c.Next()
	if err != nil || head != websockettest.Fin|websockettest.OpText {
		t.Fatalf("not a message: %#x %q %v", head, payload, err)
	}
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatal(err)
	}
	if id, _ := m["MessageId"].(string); !hexID.MatchString(id) {
		t.Errorf("MessageId %q, want a Guid as Jellyfin writes one", id)
	}
	return m
}

// An app opens its socket with its token in the query, as Jellyfin's web app does, or its header;
// it is told how often to keep it alive, and each KeepAlive is answered, what it sends besides let
// pass. One without a token is refused before any socket is opened.
func TestAnAppKeepsItsSocketAlive(t *testing.T) {
	api, _, _, _ := newAPI()
	srv := httptest.NewServer(api)
	defer srv.Close()
	for _, open := range []struct {
		target string
		header http.Header
	}{
		{srv.URL + "/socket?ApiKey=pst_device&deviceId=TW96", nil},
		{srv.URL + "/Socket", http.Header{"Authorization": {kotlin + `, Token="pst_device"`}}},
	} {
		c := openSocket(t, open.target, open.header)
		if m := heard(t, c); m["MessageType"] != "ForceKeepAlive" || m["Data"] != float64(60) {
			t.Errorf("%s: first %v, want ForceKeepAlive every 60 seconds", open.target, m)
		}
		say(t, c, `{"MessageType":"SessionsStart","Data":"0,1500"}`)
		say(t, c, `not even JSON`)
		say(t, c, `{"MessageType":"KeepAlive"}`)
		if m := heard(t, c); m["MessageType"] != "KeepAlive" {
			t.Errorf("%s: a KeepAlive answered with %v", open.target, m)
		}
	}
	_, resp, err := websockettest.Dial(t.Context(), srv.URL+"/socket?ApiKey=pst_stranger", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("an unknown token: %d, want 401", resp.StatusCode)
	}
}

// pipedResponse is a response whose connection is one end of a pipe, which synctest's clock times
// as it times no socket.
type pipedResponse struct {
	http.ResponseWriter
	conn net.Conn
	r    *bufio.Reader
}

func (p pipedResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return p.conn, bufio.NewReadWriter(p.r, bufio.NewWriter(p.conn)), nil
}

// An app's socket stays open while it keeps it alive, and is closed once the app has said nothing
// for a minute.
func TestASilentAppsSocketIsClosed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		api, _, _, _ := newAPI()
		server, client := net.Pipe()
		served := make(chan struct{})
		go func() {
			defer close(served)
			br := bufio.NewReader(server)
			r, err := http.ReadRequest(br)
			if err != nil {
				t.Error(err)
				return
			}
			api.ServeHTTP(pipedResponse{httptest.NewRecorder(), server, br}, r)
		}()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/socket?ApiKey=pst_device", nil)
		c, resp, err := websockettest.Open(client, r)
		if err != nil || c == nil {
			t.Fatalf("%v %v", resp, err)
		}
		resp.Body.Close()
		heard(t, c)
		start := time.Now()
		<-time.After(45 * time.Second)
		say(t, c, `{"MessageType":"KeepAlive"}`)
		heard(t, c)
		head, _, err := c.Next()
		if err != nil || head != websockettest.Fin|websockettest.OpClose {
			t.Fatalf("after the silence: %#x %v, want a close", head, err)
		}
		if waited := time.Since(start); waited != 45*time.Second+lostAfter {
			t.Errorf("closed after %v, want a minute after the last KeepAlive", waited)
		}
		<-served
	})
}
