// Package gzipped gzips JSON replies for the clients that take gzip.
package gzipped

import (
	"compress/gzip"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Above is the least body worth compressing: below it gzip's own framing costs as much as it
// saves.
const Above = 1 << 10

// level is what ASP.NET's response compression, which Jellyfin uses, defaults to
// (CompressionLevel.Fastest): a JSON answer is made fresh for each request.
const level = gzip.BestSpeed

var writers sync.Pool

// Accepts reports whether the client takes a reply in a content coding ("gzip", "br"): names it
// in Accept-Encoding, and not with a weight of 0 (RFC 9110 §12.5.3).
func Accepts(r *http.Request, coding string) bool {
	for e := range strings.SplitSeq(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(e, ";")
		if !strings.EqualFold(strings.TrimSpace(name), coding) {
			continue
		}
		_, weight, ok := strings.Cut(params, "q=")
		q, err := strconv.ParseFloat(strings.TrimSpace(weight), 64)
		return !ok || err != nil || q > 0
	}
	return false
}

// IsJSON reports whether a Content-Type is JSON's, or a type written as JSON (RFC 6839).
func IsJSON(contentType string) bool {
	t, _, err := mime.ParseMediaType(contentType)
	return err == nil && (t == "application/json" || strings.HasSuffix(t, "+json"))
}

// Writer is a gzip.Writer to w, from those used before; Release gives it back.
func Writer(w io.Writer) (*gzip.Writer, error) {
	if gz, ok := writers.Get().(*gzip.Writer); ok {
		gz.Reset(w)
		return gz, nil
	}
	return gzip.NewWriterLevel(w, level)
}

// Release closes gz, finishing what it writes, and keeps it for another reply.
func Release(gz *gzip.Writer) error {
	err := gz.Close()
	writers.Put(gz)
	return err
}

// JSON gzips each JSON reply of Above or more that h writes, as it is written, for a client that
// takes gzip. What else h writes, and every reply to a client that does not, is left as it is.
func JSON(h http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if !Accepts(r, "gzip") {
			h.ServeHTTP(w, r)
			return
		}
		z := &writer{ResponseWriter: w}
		h.ServeHTTP(z, r)
		// A write fails only once the client has gone, and then there is no one left to tell.
		if err := z.finish(); err != nil {
			log.DebugContext(r.Context(), "reply not written", slog.Any("err", err))
		}
	})
}

// mode is what a writer has made of its reply.
type mode int

const (
	// holding is a JSON reply not yet long enough to be worth compressing.
	holding mode = iota
	plain
	zipped
)

type writer struct {
	http.ResponseWriter
	mode   mode
	status int
	held   []byte
	gz     *gzip.Writer
}

func (z *writer) Unwrap() http.ResponseWriter { return z.ResponseWriter }

func (z *writer) WriteHeader(status int) {
	if z.status == 0 {
		z.status = status
	}
}

func (z *writer) Write(p []byte) (int, error) {
	if z.status == 0 {
		z.status = http.StatusOK
	}
	if z.mode == holding {
		h := z.Header()
		if z.status == http.StatusOK && IsJSON(h.Get("Content-Type")) && h.Get("Content-Encoding") == "" {
			z.held = append(z.held, p...)
			if len(z.held) < Above {
				return len(p), nil
			}
			return len(p), z.become(zipped)
		}
		if err := z.become(plain); err != nil {
			return 0, err
		}
	}
	if z.mode == zipped {
		return z.gz.Write(p)
	}
	return z.ResponseWriter.Write(p)
}

// become sends the header as m has it, and what was held.
func (z *writer) become(m mode) error {
	z.mode = m
	if m == zipped {
		z.Header().Set("Content-Encoding", "gzip")
		z.Header().Del("Content-Length")
	}
	z.ResponseWriter.WriteHeader(z.status)
	held := z.held
	z.held = nil
	if m == plain {
		if len(held) == 0 {
			return nil
		}
		_, err := z.ResponseWriter.Write(held)
		return err
	}
	var err error
	if z.gz, err = Writer(z.ResponseWriter); err != nil {
		return err
	}
	_, err = z.gz.Write(held)
	return err
}

// finish sends a reply too short to compress as it is, and ends a gzipped one.
func (z *writer) finish() error {
	switch z.mode {
	case holding:
		if z.status == 0 {
			return nil
		}
		return z.become(plain)
	case zipped:
		return Release(z.gz)
	case plain:
	}
	return nil
}
