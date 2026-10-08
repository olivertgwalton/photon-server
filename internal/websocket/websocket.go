// Package websocket accepts a WebSocket (RFC 6455) on a server's request: it reads a client's
// messages, in as many fragments as the client sends them, answers its pings, and writes the
// server's text messages, each in one frame. It offers no extensions and no subprotocols.
package websocket

import (
	"bufio"
	"crypto/sha1" //nolint:gosec // RFC 6455 names SHA-1 for the handshake, which secures nothing
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// MaxMessage is the most a client's message may be, its fragments together.
	MaxMessage = 64 << 10
	// writeWithin is how long a frame has to be written before the client is taken to be gone.
	writeWithin = 10 * time.Second
	// maxControl is the most a ping's, pong's or close's payload may be.
	maxControl = 125
	acceptGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

type opcode byte

const (
	opContinuation opcode = 0x0
	opText         opcode = 0x1
	opBinary       opcode = 0x2
	opClose        opcode = 0x8
	opPing         opcode = 0x9
	opPong         opcode = 0xA
)

// Status is why a WebSocket is closed, as its close frame says.
type Status uint16

const (
	StatusNormal        Status = 1000
	StatusGoingAway     Status = 1001
	StatusProtocolError Status = 1002
	StatusInvalidData   Status = 1007
	StatusTooBig        Status = 1009
)

// ErrNotWebSocket is a request that does not ask for a WebSocket, answered 400, or 426 where it
// asks for a version other than 13.
var ErrNotWebSocket = errors.New("websocket: not a websocket handshake")

// Conn is a WebSocket the server accepted. Read is for one goroutine; Write and Close may be called
// from any, beside it.
type Conn struct {
	conn net.Conn
	r    *bufio.Reader
	// mu is held while a frame is written: a pong or a close may be written while a message is.
	mu sync.Mutex
}

// Accept answers a client's handshake from any origin and takes the connection from the server.
func Accept(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	nonce, err := base64.StdEncoding.DecodeString(key)
	if r.Method != http.MethodGet || err != nil || len(nonce) != 16 ||
		!hasToken(r.Header, "Connection", "upgrade") || !hasToken(r.Header, "Upgrade", "websocket") {
		w.WriteHeader(http.StatusBadRequest)
		return nil, ErrNotWebSocket
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		w.WriteHeader(http.StatusUpgradeRequired)
		return nil, ErrNotWebSocket
	}
	conn, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum([]byte(key + acceptGUID)) //nolint:gosec // as the import says
	c := &Conn{conn: conn, r: rw.Reader}
	// The server's read deadline for the request's header would end the socket.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, err
	}
	err = c.send([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n"))
	if err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

// hasToken is whether a header lists token, its case ignored.
func hasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for t := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// Read answers the client's next message, failing once the client has sent nothing for quiet.
// It answers pings as it reads, and io.EOF once the client closes. A client that breaks the
// protocol, or sends more than MaxMessage, is told why and closed.
func (c *Conn) Read(quiet time.Duration) ([]byte, error) {
	var msg []byte
	var kind opcode
	for {
		if err := c.conn.SetReadDeadline(time.Now().Add(quiet)); err != nil {
			return nil, err
		}
		fin, op, payload, err := c.frame(MaxMessage - len(msg))
		if err != nil {
			return nil, err
		}
		switch op {
		case opPing:
			if err := c.write(opPong, payload); err != nil {
				return nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			if len(payload) == 1 {
				return nil, c.fail(StatusProtocolError)
			}
			// Answered with its own status, as the RFC asks.
			if err := errors.Join(c.write(opClose, payload[:min(len(payload), 2)]), c.conn.Close()); err != nil {
				return nil, err
			}
			return nil, io.EOF
		case opText, opBinary:
			if kind != 0 {
				return nil, c.fail(StatusProtocolError)
			}
			kind = op
		case opContinuation:
			if kind == 0 {
				return nil, c.fail(StatusProtocolError)
			}
		default:
			return nil, c.fail(StatusProtocolError)
		}
		msg = append(msg, payload...)
		if !fin {
			continue
		}
		if kind == opText && !utf8.Valid(msg) {
			return nil, c.fail(StatusInvalidData)
		}
		return msg, nil
	}
}

// frame reads a frame, of at most room bytes where it is part of a message.
func (c *Conn) frame(room int) (fin bool, op opcode, payload []byte, err error) {
	var head [2]byte
	if _, err := io.ReadFull(c.r, head[:]); err != nil {
		return false, 0, nil, err
	}
	fin, op = head[0]&0x80 != 0, opcode(head[0]&0x0f)
	control := op >= opClose
	n := uint64(head[1] & 0x7f)
	// A client masks every frame, and sets no bit an extension would.
	if head[0]&0x70 != 0 || head[1]&0x80 == 0 || control && (!fin || n > maxControl) {
		return false, 0, nil, c.fail(StatusProtocolError)
	}
	switch n {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		n = binary.BigEndian.Uint64(ext[:])
	}
	if !control && n > uint64(room) {
		return false, 0, nil, c.fail(StatusTooBig)
	}
	var mask [4]byte
	if _, err := io.ReadFull(c.r, mask[:]); err != nil {
		return false, 0, nil, err
	}
	payload = make([]byte, n)
	if _, err := io.ReadFull(c.r, payload); err != nil {
		return false, 0, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return fin, op, payload, nil
}

// fail tells the client why it is closed, and closes it.
func (c *Conn) fail(s Status) error {
	return errors.Join(fmt.Errorf("websocket: client closed with %d", s), c.Close(s))
}

// Write writes a text message.
func (c *Conn) Write(text []byte) error {
	return c.write(opText, text)
}

// Close tells the client why, where it is still there, and closes the connection.
func (c *Conn) Close(s Status) error {
	return errors.Join(c.write(opClose, binary.BigEndian.AppendUint16(nil, uint16(s))), c.conn.Close())
}

func (c *Conn) write(op opcode, payload []byte) error {
	head := []byte{0x80 | byte(op)}
	switch n := len(payload); {
	case n <= maxControl:
		head = append(head, byte(n))
	case n <= math.MaxUint16:
		head = binary.BigEndian.AppendUint16(append(head, 126), uint16(n))
	default:
		head = binary.BigEndian.AppendUint64(append(head, 127), uint64(n))
	}
	return c.send(append(head, payload...))
}

func (c *Conn) send(b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.SetWriteDeadline(time.Now().Add(writeWithin)); err != nil {
		return err
	}
	_, err := c.conn.Write(b)
	return err
}
