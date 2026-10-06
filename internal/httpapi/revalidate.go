package httpapi

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
)

// revalidate tags a JSON answer to a GET with a weak ETag of its body, and answers a client that
// already holds that body 304 Not Modified with none. The tag hashes the body as the handler wrote
// it, before compressJSON, so it is the same gzipped or not.
func revalidate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bw := &bufferingWriter{ResponseWriter: w}
		next.ServeHTTP(bw, r)
		if bw.status != http.StatusOK {
			return
		}
		h := w.Header()
		if !isJSON(h.Get("Content-Type")) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(bw.body.Bytes())
			return
		}
		// FNV-1a is in the standard library, quick, and the same on every server and after every
		// restart; telling one version of an answer from the next needs nothing stronger.
		sum := fnv.New64a()
		_, _ = sum.Write(bw.body.Bytes())
		tag := fmt.Sprintf(`W/"%016x"`, sum.Sum64())
		h.Set("ETag", tag)
		// An answer is the signed-in profile's own, and may change at any moment.
		h.Set("Cache-Control", "private, no-cache")
		if matchesAny(r.Header.Get("If-None-Match"), tag) {
			h.Del("Content-Type")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bw.body.Bytes())
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

// bufferingWriter holds back a 200 answer whole and lets any other through as it is written.
type bufferingWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (b *bufferingWriter) Unwrap() http.ResponseWriter { return b.ResponseWriter }

func (b *bufferingWriter) WriteHeader(status int) {
	if b.status != 0 {
		return
	}
	b.status = status
	if status != http.StatusOK {
		b.ResponseWriter.WriteHeader(status)
	}
}

func (b *bufferingWriter) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.WriteHeader(http.StatusOK)
	}
	if b.status != http.StatusOK {
		return b.ResponseWriter.Write(p)
	}
	return b.body.Write(p)
}
