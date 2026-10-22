// Package gzipped gzips replies for the clients that take gzip.
package gzipped

import (
	"compress/gzip"
	"io"
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
