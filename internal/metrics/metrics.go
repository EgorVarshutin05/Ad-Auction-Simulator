package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var Requests = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "auction_http_requests_total",
	Help: "Total HTTP requests"},
	[]string{"method", "path", "status"})

var Latency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "auction_http_request_duration_seconds",
	Help:    "HTTP request latency",
	Buckets: prometheus.DefBuckets,
}, []string{"path"})

var BidAmount = prometheus.NewSummary(prometheus.SummaryOpts{
	Name: "auction_bid_amount",
	Help: "Bid amount observations",
})

func init() {
	prometheus.MustRegister(Requests, Latency, BidAmount)
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		wrapped := &responseWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(wrapped, r)
		path := r.URL.Path

		if path != "/bid" && path != "/winner" && path != "/stats" {
			path = "other"
		}

		Requests.WithLabelValues(r.Method, path, strconv.Itoa(wrapped.status)).Inc()
		Latency.WithLabelValues(path).Observe(time.Since(start).Seconds())
	})
}
