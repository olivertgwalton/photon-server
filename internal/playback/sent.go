package playback

import (
	"context"
	"io"
	"math"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	// sentChunk is how much of a file is sent between counts of what has been sent.
	sentChunk = 4 << 20
	// measureEvery is how often a node measures how fast it sends media: its rate is that of the
	// last, so a stream begun since is not yet in it.
	measureEvery = 5 * time.Second
)

// Delivery is how media is sent to a player: a file as it is, or HLS made for it.
type Delivery string

const (
	DeliveryFile    Delivery = "file"
	DeliverySegment Delivery = "segment"
)

func deliveries() []Delivery { return []Delivery{DeliveryFile, DeliverySegment} }

// Sent counts the bytes of media a node sends its players, whichever API they play by, and
// measures how fast it sends them.
type Sent struct {
	bytes *prometheus.CounterVec
	total atomic.Int64
	kbps  atomic.Int64
	// last is the total when it was last measured, at.
	last int64
	at   time.Time
}

func NewSent() *Sent {
	bytes := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "photon_sent_bytes_total", Help: "The bytes of media this node sent its players, by how each was delivered.",
	}, []string{"delivery"})
	for _, d := range deliveries() {
		bytes.WithLabelValues(string(d))
	}
	return &Sent{bytes: bytes}
}

// Measure measures how fast media is sent every measureEvery until ctx ends.
func (s *Sent) Measure(ctx context.Context) {
	t := time.NewTicker(measureEvery)
	defer t.Stop()
	s.last, s.at = s.total.Load(), time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.measure(now)
		}
	}
}

// measure keeps how fast media was sent since it was last measured.
func (s *Sent) measure(now time.Time) {
	total := s.total.Load()
	if ms := now.Sub(s.at).Milliseconds(); ms > 0 {
		s.kbps.Store((total - s.last) * 8 / ms)
	}
	s.last, s.at = total, now
}

// Kbps is how fast media was sent, in kilobits a second, as last measured.
func (s *Sent) Kbps() int { return int(s.kbps.Load()) }

func (s *Sent) add(n int64) { s.total.Add(n) }

func (s *Sent) Describe(ch chan<- *prometheus.Desc) { s.bytes.Describe(ch) }

func (s *Sent) Collect(ch chan<- prometheus.Metric) { s.bytes.Collect(ch) }

// Counting counts what next writes as media of delivery.
func (s *Sent) Counting(d Delivery, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&counting{ResponseWriter: w, sent: s.bytes.WithLabelValues(string(d)), total: s}, r)
	})
}

// counting is a response counting its body as it is written. It hands a file to the connection's
// own ReadFrom, so http.ServeContent still sends it with sendfile, and unwraps for
// http.ResponseController, which a direct play's stop uses to cut it off.
type counting struct {
	http.ResponseWriter
	sent  prometheus.Counter
	total *Sent
}

func (c *counting) Write(b []byte) (int, error) {
	n, err := c.ResponseWriter.Write(b)
	c.count(int64(n))
	return n, err
}

func (c *counting) count(n int64) {
	c.sent.Add(float64(n))
	c.total.add(n)
}

// ReadFrom hands src on a chunk at a time, counting each as it is sent, so a play hours long is
// counted as it goes rather than when it ends. Each chunk is one LimitedReader around src's own
// reader, the one wrapping net's sendfile unwraps to reach a file.
func (c *counting) ReadFrom(src io.Reader) (int64, error) {
	lr, ok := src.(*io.LimitedReader)
	if !ok {
		lr = &io.LimitedReader{R: src, N: math.MaxInt64}
	}
	var sent int64
	for lr.N > 0 {
		want := min(sentChunk, lr.N)
		n, err := c.readFrom(&io.LimitedReader{R: lr.R, N: want})
		lr.N -= n
		sent += n
		c.count(n)
		if err != nil || n < want {
			return sent, err
		}
	}
	return sent, nil
}

func (c *counting) readFrom(r io.Reader) (int64, error) {
	if rf, ok := c.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(struct{ io.Writer }{c.ResponseWriter}, r)
}

func (c *counting) Flush() { _ = http.NewResponseController(c.ResponseWriter).Flush() }

func (c *counting) Unwrap() http.ResponseWriter { return c.ResponseWriter }
