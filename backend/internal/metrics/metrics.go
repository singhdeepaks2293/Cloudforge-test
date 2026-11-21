// Package metrics exposes Prometheus metrics for the CloudForge API
// (Milestone 9).
//
// Exported series (no user-identifying or secret labels; the route
// label is the chi route pattern, e.g. /api/v1/labs/{id}, never the
// raw path, so high-cardinality ids cannot explode the series count):
//
//	cloudforge_http_requests_total{method,route,status}
//	cloudforge_http_request_duration_seconds{method,route}
//	cloudforge_network_engine_up (1 = engine gRPC Health ok, 0 = down)
//	cloudforge_host_agent_up (1 = host-agent /healthz ok, 0 = down or unconfigured)
//	cloudforge_lab_events_total{event} (lab_created, lab_destroyed — M9 Part 3)
//	cloudforge_jobs_total{type,result} (result: completed|failed|dead — M9 Part 3)
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// RequestsTotal counts completed HTTP requests by method, chi route
// pattern and response status code.
var RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "cloudforge",
	Subsystem: "http",
	Name:      "requests_total",
	Help:      "Total HTTP requests by method, route pattern and status code.",
}, []string{"method", "route", "status"})

// RequestDuration observes request latency by method and route pattern.
var RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: "cloudforge",
	Subsystem: "http",
	Name:      "request_duration_seconds",
	Help:      "HTTP request latency in seconds by method and route pattern.",
	Buckets:   prometheus.DefBuckets,
}, []string{"method", "route"})

// EngineUp reports the last observed network-engine gRPC health
// (1 = healthy, 0 = down or unconfigured). Updated by /readyz.
var EngineUp = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: "cloudforge",
	Subsystem: "network_engine",
	Name:      "up",
	Help:      "Whether the Rust network engine reports healthy over gRPC (1) or not (0).",
})

// SetEngineUp records the engine health observed by a readiness check.
func SetEngineUp(up bool) {
	if up {
		EngineUp.Set(1)
	} else {
		EngineUp.Set(0)
	}
}

// HostAgentUp reports the last observed host-agent health
// (1 = /healthz ok, 0 = down or unconfigured). Updated by
// /api/v1/health.
var HostAgentUp = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: "cloudforge",
	Subsystem: "host_agent",
	Name:      "up",
	Help:      "Whether the host agent answers /healthz (1) or not (0).",
})

// SetHostAgentUp records the host-agent health observed by a health check.
func SetHostAgentUp(up bool) {
	if up {
		HostAgentUp.Set(1)
	} else {
		HostAgentUp.Set(0)
	}
}

// LabEventsTotal counts lab lifecycle events observed by the API.
// Event is lab_created or lab_destroyed. (Provisioning outcomes are
// worker-side and surface through cloudforge_jobs_total.)
var LabEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "cloudforge",
	Subsystem: "lab",
	Name:      "events_total",
	Help:      "Total lab lifecycle events observed by the API, by event.",
}, []string{"event"})

// JobsTotal counts worker job outcomes by job type and result
// (completed, failed, dead).
var JobsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "cloudforge",
	Subsystem: "jobs",
	Name:      "total",
	Help:      "Total worker jobs handled, by type and result.",
}, []string{"type", "result"})

// Handler serves the Prometheus scrape endpoint (GET /metrics).
func Handler() http.Handler { return promhttp.Handler() }
