package playback

import (
	"io"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
)

// Delivery is how media is sent to a player: a file as it is, or HLS made for it.
type Delivery string

const (
	DeliveryFile    Delivery = "file"
	DeliverySegment Delivery = "segment"
)

func deliveries() []Delivery { return []Delivery{DeliveryFile, DeliverySegment} }

// Sent counts the bytes of media a node sends its players, whichever API they play by.
type Sent struct{ bytes *prometheus.CounterVec }

func NewSent() *Sent {
	bytes := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "photon_sent_bytes_total", Help: "The bytes of media this node sent its players, by how each was delivered.",
	}, []string{"delivery"})
	for _, d := range deliveries() {
		bytes.WithLabelValues(string(d))
	}
	return &Sent{bytes}
}

func (s *Sent) Describe(ch chan<- *prometheus.Desc) { s.bytes.Describe(ch) }

func (s *Sent) Collect(ch chan<- prometheus.Metric) { s.bytes.Collect(ch) }

// Counting counts what next writes as media of delivery.
func (s *Sent) Counting(d Delivery, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&counting{ResponseWriter: w, sent: s.bytes.WithLabelValues(string(d))}, r)
	})
}

// counting is a response counting its body as it is written. It hands a file to the connection's
// own ReadFrom, so http.ServeContent still sends it with sendfile, and unwraps for
// http.ResponseController, which a direct play's stop uses to cut it off.
type counting struct {
	http.ResponseWriter
	sent prometheus.Counter
}

func (c *counting) Write(b []byte) (int, error) {
	n, err := c.ResponseWriter.Write(b)
	c.sent.Add(float64(n))
	return n, err
}

func (c *counting) ReadFrom(r io.Reader) (int64, error) {
	var n int64
	var err error
	if rf, ok := c.ResponseWriter.(io.ReaderFrom); ok {
		n, err = rf.ReadFrom(r)
	} else {
		n, err = io.Copy(struct{ io.Writer }{c.ResponseWriter}, r)
	}
	c.sent.Add(float64(n))
	return n, err
}

func (c *counting) Flush() { _ = http.NewResponseController(c.ResponseWriter).Flush() }

func (c *counting) Unwrap() http.ResponseWriter { return c.ResponseWriter }
