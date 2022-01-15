package instance

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Prometheus interface {
	Register(prometheus.Registerer)

	CurrentStreamCount() prometheus.Gauge
	TotalStreamDurationSeconds() prometheus.Histogram
}
