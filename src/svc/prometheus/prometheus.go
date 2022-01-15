package prometheus

import (
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/configure"
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/instance"

	"github.com/prometheus/client_golang/prometheus"
)

type mon struct {
	currentStreamCount         prometheus.Gauge
	totalStreamDurationSeconds prometheus.Histogram
}

func (m *mon) Register(r prometheus.Registerer) {
	r.MustRegister(
		m.currentStreamCount,
		m.totalStreamDurationSeconds,
	)
}

func (m *mon) CurrentStreamCount() prometheus.Gauge {
	return m.currentStreamCount
}

func (m *mon) TotalStreamDurationSeconds() prometheus.Histogram {
	return m.totalStreamDurationSeconds
}

func LabelsFromKeyValue(kv []configure.KeyValue) prometheus.Labels {
	mp := prometheus.Labels{}

	for _, v := range kv {
		mp[v.Key] = v.Value
	}

	return mp
}

func New(opts SetupOptions) instance.Prometheus {
	return &mon{
		currentStreamCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "transmuxer_current_stream_count",
			Help:        "The number of rtmp streams being consumed",
			ConstLabels: opts.Labels,
		}),
		totalStreamDurationSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:        "transmuxer_total_stream_duration_seconds",
			Help:        "The total seconds occupied consuming streams",
			ConstLabels: opts.Labels,
		}),
	}
}

type SetupOptions struct {
	Labels prometheus.Labels
}
