package httpapi

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"sync"
)

// compressAbove is the least body worth compressing: below it gzip's own framing costs as much as
// it saves.
const compressAbove = 1 << 10

// gzipLevel is what ASP.NET's response compression, which Jellyfin uses, defaults to
// (CompressionLevel.Fastest): a JSON answer is made fresh for each request.
const gzipLevel = gzip.BestSpeed

var gzipWriters sync.Pool

// revalidate holds a reply back whole. It tags a JSON answer to a GET with a weak ETag of its
// body, and answers a client that already holds that body 304 Not Modified with none; the tag
// hashes the body before it is gzipped, so it is the same either way. It gzips a JSON reply of
// compressAbove or more for a client that takes gzip.
func (a *API) revalidate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		bw := &bufferingWriter{ResponseWriter: w}
		next.ServeHTTP(bw, r)
		if bw.status == 0 {
			return
		}
		h := w.Header()
		body := bw.body.Bytes()
		json := isJSON(h.Get("Content-Type"))
		if json && bw.status == http.StatusOK && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			// FNV-1a is in the standard library, quick, and the same on every server and after
			// every restart; telling one version of an answer from the next needs nothing stronger.
			sum := fnv.New64a()
			// A hash's Write never fails.
			sum.Write(body)
			tag := fmt.Sprintf(`W/"%016x"`, sum.Sum64())
			h.Set("ETag", tag)
			// An answer is the signed-in profile's own, and may change at any moment.
			h.Set("Cache-Control", "private, no-cache")
			if matchesAny(r.Header.Get("If-None-Match"), tag) {
				h.Del("Content-Type")
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		if !json || len(body) < compressAbove || h.Get("Content-Encoding") != "" || !accepts(r, "gzip") {
			w.WriteHeader(bw.status)
			writeBody(w, a.logger, body)
			return
		}
		h.Set("Content-Encoding", "gzip")
		h.Del("Content-Length")
		gz, ok := gzipWriters.Get().(*gzip.Writer)
		if ok {
			gz.Reset(w)
		} else {
			var err error
			if gz, err = gzip.NewWriterLevel(w, gzipLevel); err != nil {
				a.logger.DebugContext(r.Context(), "reply not written", slog.Any("err", err))
				return
			}
		}
		w.WriteHeader(bw.status)
		_, err := gz.Write(body)
		if err = errors.Join(err, gz.Close()); err != nil {
			a.logger.DebugContext(r.Context(), "reply not written", slog.Any("err", err))
		}
		gzipWriters.Put(gz)
	})
}

// matchesAny says whether an If-None-Match list names tag, comparing weakly as RFC 9110 §13.1.2
// has it: W/ is ignored.
func matchesAny(list, tag string) bool {
	tag = strings.TrimPrefix(tag, "W/")
	for t := range strings.SplitSeq(list, ",") {
		t = strings.TrimSpace(t)
		if t == "*" || strings.TrimPrefix(t, "W/") == tag {
			return true
		}
	}
	return false
}

func isJSON(contentType string) bool {
	t, _, err := mime.ParseMediaType(contentType)
	return err == nil && (t == "application/json" || t == "application/problem+json")
}

// bufferingWriter holds back the status and the body until the handler is done.
type bufferingWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (b *bufferingWriter) Unwrap() http.ResponseWriter { return b.ResponseWriter }

func (b *bufferingWriter) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}

func (b *bufferingWriter) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}
