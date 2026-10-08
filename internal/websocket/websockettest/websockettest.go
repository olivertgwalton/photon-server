// Package websockettest is a WebSocket client for tests, written apart from the server it tests:
// it sends frames as raw as a test needs and reads the server's as they come.
package websockettest

import (
	"bufio"
	"context"
	"crypto/sha1" //nolint:gosec // RFC 6455 names SHA-1 for the handshake, which secures nothing
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Op is a frame's opcode, and Fin its last fragment's bit.
const (
	OpContinuation byte = 0x0
	OpText         byte = 0x1
	OpClose        byte = 0x8
	OpPing         byte = 0x9
	OpPong         byte = 0xA
	Fin            byte = 0x80
)

const key = "dGhlIHNhbXBsZSBub25jZQ=="

// Client is a client's side of a WebSocket; its connection is there for what a test writes raw.
type Client struct {
	net.Conn
	r *bufio.Reader
}

// Dial opens a WebSocket at target, an http:// address, with header, and gives up on it after a
// few seconds so no test hangs. The response is the server's answer to the handshake.
func Dial(ctx context.Context, target string, header http.Header) (*Client, *http.Response, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, nil, err
	}
	conn, err := new(net.Dialer).DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, nil, err
	}
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, nil, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
	if err != nil {
		return nil, nil, err
	}
	maps.Copy(r.Header, header)
	c, resp, err := Open(conn, r)
	if c == nil {
		conn.Close()
	}
	return c, resp, err
}

// Open sends r over conn as a WebSocket's handshake, and reads the answer. A client is made only
// where the server switches protocols.
func Open(conn net.Conn, r *http.Request) (*Client, *http.Response, error) {
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Sec-WebSocket-Key", key)
	r.Header.Set("Sec-WebSocket-Version", "13")
	if err := r.Write(conn); err != nil {
		return nil, nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, r)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, resp, resp.Body.Close()
	}
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")) //nolint:gosec // as the import says
	if resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		return nil, resp, errors.New("websockettest: the server's accept is not the key's")
	}
	return &Client{Conn: conn, r: br}, resp, nil
}

// Send writes a frame masked as a client's are, its first byte head: Fin and an opcode.
func (c *Client) Send(head byte, payload []byte) error {
	b := []byte{head}
	switch n := len(payload); {
	case n < 126:
		b = append(b, 0x80|byte(n))
	case n <= 0xffff:
		b = binary.BigEndian.AppendUint16(append(b, 0x80|126), uint16(n))
	default:
		b = binary.BigEndian.AppendUint64(append(b, 0x80|127), uint64(n))
	}
	mask := [4]byte{0x37, 0xfa, 0x21, 0x3d}
	b = append(b, mask[:]...)
	for i, p := range payload {
		b = append(b, p^mask[i%4])
	}
	_, err := c.Write(b)
	return err
}

// Next reads the server's next frame: its first byte, and its payload.
func (c *Client) Next() (head byte, payload []byte, err error) {
	var h [2]byte
	if _, err := io.ReadFull(c.r, h[:]); err != nil {
		return 0, nil, err
	}
	if h[1]&0x80 != 0 {
		return 0, nil, errors.New("websockettest: the server masked a frame")
	}
	n := uint64(h[1])
	switch n {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.r, ext[:]); err != nil {
			return 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.r, ext[:]); err != nil {
			return 0, nil, err
		}
		n = binary.BigEndian.Uint64(ext[:])
	}
	payload = make([]byte, n)
	_, err = io.ReadFull(c.r, payload)
	return h[0], payload, err
}
