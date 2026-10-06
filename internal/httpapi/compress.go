package httpapi

import (
	"compress/gzip"
	"mime"
	"net/http"
	"sync"
)

// compressAbove is the least body worth compressing: below it gzip's own framing costs as much as
// it saves.
const compressAbove = 1 << 10

// gzipLevel is what ASP.NET's response compression, which Jellyfin uses, defaults to
// (CompressionLevel.Fastest): a JSON answer is made fresh for each request.
const gzipLevel = gzip.BestSpeed

var gzipWriters sync.Pool

// compressJSON gzips a JSON answer of compressAbove or more for a client that takes gzip.
func compressJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if !accepts(r, "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		cw := &compressingWriter{ResponseWriter: w}
		defer cw.close()
		next.ServeHTTP(cw, r)
	})
}

// compressingWriter holds back the status and the start of the body until it knows whether the
// answer is JSON enough to compress.
type compressingWriter struct {
	http.ResponseWriter
	status  int
	start   []byte
	decided bool
	gz      *gzip.Writer
}

func (c *compressingWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func (c *compressingWriter) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

func (c *compressingWriter) Write(p []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	switch {
	case c.gz != nil:
		return c.gz.Write(p)
	case c.decided:
		return c.ResponseWriter.Write(p)
	}
	c.start = append(c.start, p...)
	if len(c.start) >= compressAbove {
		if err := c.decide(); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (c *compressingWriter) decide() error {
	c.decided = true
	h := c.Header()
	if len(c.start) >= compressAbove && h.Get("Content-Encoding") == "" && isJSON(h.Get("Content-Type")) {
		h.Set("Content-Encoding", "gzip")
		h.Del("Content-Length")
		if gz, ok := gzipWriters.Get().(*gzip.Writer); ok {
			gz.Reset(c.ResponseWriter)
			c.gz = gz
		} else {
			c.gz, _ = gzip.NewWriterLevel(c.ResponseWriter, gzipLevel)
		}
	}
	c.ResponseWriter.WriteHeader(c.status)
	if c.gz != nil {
		_, err := c.gz.Write(c.start)
		return err
	}
	_, err := c.ResponseWriter.Write(c.start)
	return err
}

func (c *compressingWriter) close() {
	if c.status == 0 {
		return
	}
	if !c.decided {
		_ = c.decide()
	}
	if c.gz != nil {
		_ = c.gz.Close()
		gzipWriters.Put(c.gz)
	}
}

func isJSON(contentType string) bool {
	t, _, _ := mime.ParseMediaType(contentType)
	return t == "application/json" || t == "application/problem+json"
}
