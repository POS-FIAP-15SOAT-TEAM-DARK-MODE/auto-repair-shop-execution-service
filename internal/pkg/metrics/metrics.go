package metrics

import (
	"context"
	"math"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

var HTTPRequestDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds, by method, route and status.",
		Buckets: prometheus.DefBuckets,
	},
	[]string{"method", "route", "status"},
)

// scrapeTimeout bounds the query behind a gauge read on each scrape.
const scrapeTimeout = 2 * time.Second

func init() {
	prometheus.MustRegister(HTTPRequestDuration)
}

// RegisterOutboxPending registers the outbox_pending gauge on reg. The value is read with
// count on every scrape, so it needs no background loop. If the query fails the gauge is NaN
// (not 0), so a broken database is not mistaken for an empty outbox.
func RegisterOutboxPending(reg prometheus.Registerer, count func(context.Context) (int64, error)) error {
	return reg.Register(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "outbox_pending",
			Help: "Outbox rows not yet published to the broker.",
		},
		func() float64 {
			ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
			defer cancel()
			n, err := count(ctx)
			if err != nil {
				logger.Global().Error(err, zap.String("metric", "outbox_pending"))
				return math.NaN()
			}
			return float64(n)
		},
	))
}
