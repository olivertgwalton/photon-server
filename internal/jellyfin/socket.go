package jellyfin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/websocket"
)

// lostAfter is how long an app's socket may say nothing before it is closed, as Jellyfin waits.
// An app is told it, and keeps the socket alive at half of it.
const lostAfter = 60 * time.Second

// messageType is what a message on the socket is, as Jellyfin's SessionMessageType names it.
type messageType string

const (
	messageForceKeepAlive messageType = "ForceKeepAlive"
	messageKeepAlive      messageType = "KeepAlive"
)

// message is Jellyfin's WebSocketMessage.
type message struct {
	MessageType messageType `json:"MessageType"`
	Data        any         `json:"Data,omitempty"`
	MessageID   string      `json:"MessageId"`
}

// stoppingKey holds, in a request's context, what closes as its listener stops serving: Shutdown
// neither waits for nor closes a socket, which the server stops tracking once it is hijacked.
type stoppingKey struct{}

// socket keeps a WebSocket open to the signed-in app, which keeps it alive. What the app sends
// that is not a KeepAlive, such as SessionsStart, is let pass: none of it is served.
func (a *API) socket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stopping, _ := ctx.Value(stoppingKey{}).(<-chan struct{})
	c, err := websocket.Accept(w, r)
	if err != nil {
		a.logger.DebugContext(ctx, "jellyfin socket not opened", slog.Any("err", err))
		return
	}
	defer a.closeSocket(ctx, c)
	heard, done := make(chan messageType), make(chan struct{})
	defer close(done)
	go a.listen(ctx, c, heard, done)
	if !a.send(ctx, c, message{MessageType: messageForceKeepAlive, Data: int(lostAfter / time.Second)}) {
		return
	}
	for {
		select {
		case <-stopping:
			return
		case t, open := <-heard:
			if !open {
				return
			}
			if t == messageKeepAlive && !a.send(ctx, c, message{MessageType: messageKeepAlive}) {
				return
			}
		}
	}
}

// listen tells heard what kind of message the app sends, until it goes, breaks the protocol or
// says nothing for lostAfter, or done.
func (a *API) listen(ctx context.Context, c *websocket.Conn, heard chan<- messageType, done <-chan struct{}) {
	defer close(heard)
	for {
		m, err := c.Read(lostAfter)
		if err != nil {
			a.logger.DebugContext(ctx, "jellyfin socket ended", slog.Any("err", err))
			return
		}
		var in message
		if json.Unmarshal(m, &in) != nil {
			continue
		}
		select {
		case heard <- in.MessageType:
		case <-done:
			return
		}
	}
}

func (a *API) closeSocket(ctx context.Context, c *websocket.Conn) {
	if err := c.Close(websocket.StatusGoingAway); err != nil {
		a.logger.DebugContext(ctx, "jellyfin socket not closed cleanly", slog.Any("err", err))
	}
}

// send writes a message under an id of its own, as Jellyfin writes every one.
func (a *API) send(ctx context.Context, c *websocket.Conn, m message) bool {
	m.MessageID = guid(uuid.NewV7())
	body, err := json.Marshal(m)
	if err != nil {
		a.logger.ErrorContext(ctx, "jellyfin message not encoded", slog.Any("err", err))
		return false
	}
	if err := c.Write(body); err != nil {
		a.logger.DebugContext(ctx, "jellyfin message not sent", slog.Any("err", err))
		return false
	}
	return true
}
