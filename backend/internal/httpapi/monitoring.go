// CloudWatch-style monitoring + CloudTrail-style audit UI backend
// (Phase 2).
//
// GET /api/v1/monitoring returns live data only: the API's real
// Prometheus series (gathered from the default registry — request
// counts/latency by route, engine/host-agent health gauges, lab and
// job events) plus the same live component checks /readyz runs
// (Postgres/Redis/engine). No metric is synthesised; a series that
// has never been observed is absent, not zero-filled with fake data.
//
// Alarms are threshold rules evaluated live against the current
// gathered values. Alarm definitions are stored in memory on the
// server (lost on restart — the UI and docs say so); evaluation is
// real, not faked.
//
// Audit export (GET /api/v1/audit/export) streams the same filtered
// audit log the CloudTrail console lists, as CSV or JSON. Admin-only.
package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cloudforge/cloudforge/backend/internal/metrics"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// readiness runs the live component checks /readyz reports:
// Postgres (gating when configured), Redis and the network engine
// (reported, never gating). It is shared by /readyz and /monitoring
// so the two can never disagree.
func (s *Server) readiness(ctx context.Context) (string, map[string]componentHealth, error) {
	components := map[string]componentHealth{}

	start := time.Now()
	pgErr := error(nil)
	if s.DBHealth == nil {
		components["postgres"] = componentHealth{Status: "disabled", LatencyMs: time.Since(start).Milliseconds(), Error: "postgres not configured (in-memory fallback)"}
	} else {
		pgErr = s.DBHealth(ctx)
		c := componentHealth{Status: "ok", LatencyMs: time.Since(start).Milliseconds()}
		if pgErr != nil {
			c.Status = "down"
			c.Error = pgErr.Error()
		}
		components["postgres"] = c
	}

	start = time.Now()
	if s.Cache == nil || !s.Cache.Available() {
		components["redis"] = componentHealth{Status: "disabled", LatencyMs: time.Since(start).Milliseconds(), Error: "redis not configured or unavailable"}
	} else {
		ok := s.Cache.Ping(ctx)
		c := componentHealth{Status: "ok", LatencyMs: time.Since(start).Milliseconds()}
		if !ok {
			c.Status = "down"
			c.Error = "redis ping failed"
		}
		components["redis"] = c
	}

	start = time.Now()
	engineUp := false
	if s.Network == nil {
		components["engine"] = componentHealth{Status: "not_configured", LatencyMs: time.Since(start).Milliseconds(), Error: "network engine client not configured"}
	} else {
		ectx := networkclient.ContextWithRequestID(ctx, RequestID(ctx))
		st, err := s.Network.Health(ectx)
		c := componentHealth{Status: "ok", LatencyMs: time.Since(start).Milliseconds()}
		switch {
		case err != nil:
			c.Status = "down"
			c.Error = err.Error()
		case !st.OK:
			c.Status = "down"
			c.Error = st.Detail
		default:
			engineUp = true
		}
		components["engine"] = c
	}
	metrics.SetEngineUp(engineUp)

	status := "ready"
	if pgErr != nil {
		status = "not_ready"
	}
	return status, components, pgErr
}

type metricSample struct {
	Labels map[string]string `json:"labels,omitempty"`
	Value  float64           `json:"value"`
}

type metricFamily struct {
	Name    string         `json:"name"`
	Help    string         `json:"help,omitempty"`
	Type    string         `json:"type"`
	Total   float64        `json:"total"`
	Samples []metricSample `json:"samples"`
}

// gatherMetrics converts the Prometheus registry into JSON. Families
// are sorted by name; samples are capped at 200 per family so one
// high-cardinality family cannot explode the response.
func gatherMetrics() []metricFamily {
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return []metricFamily{}
	}
	out := make([]metricFamily, 0, len(families))
	for _, mf := range families {
		f := metricFamily{Name: mf.GetName(), Help: mf.GetHelp(), Type: mf.GetType().String()}
		for _, m := range mf.GetMetric() {
			var v float64
			switch {
			case m.Gauge != nil:
				v = m.Gauge.GetValue()
			case m.Counter != nil:
				v = m.Counter.GetValue()
			case m.Histogram != nil:
				v = m.Histogram.GetSampleSum()
			case m.Summary != nil:
				v = m.Summary.GetSampleSum()
			case m.Untyped != nil:
				v = m.Untyped.GetValue()
			}
			f.Total += v
			if len(f.Samples) < 200 {
				labels := map[string]string{}
				for _, lp := range m.GetLabel() {
					labels[lp.GetName()] = lp.GetValue()
				}
				f.Samples = append(f.Samples, metricSample{Labels: labels, Value: v})
			}
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// metricValue returns the summed current value of a metric family
// (0, false when the series has never been observed).
func metricValue(families []metricFamily, name string) (float64, bool) {
	for _, f := range families {
		if f.Name == name {
			return f.Total, true
		}
	}
	return 0, false
}

func (s *Server) handleMonitoring(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	status, components, _ := s.readiness(ctx)
	families := gatherMetrics()
	alarms := s.alarms().list(families)
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"status": status, "components": components,
		"metrics": families, "alarms": alarms,
		"lite": s.Lite, "mode": map[string]any{"lite": s.Lite},
		"uptime_seconds": int64(time.Since(s.Started).Seconds()),
		"version":        Version,
		"checked_at":     time.Now().UTC(),
		"note":           "Metrics are the API's real Prometheus series; a series absent here has never been observed (not zero-filled).",
	})
}

// --- Alarms ---------------------------------------------------------------

// Alarm is one threshold rule over a Prometheus metric. Definitions
// live in memory on the server (lost on restart); evaluation against
// the live gathered values is real.
type Alarm struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Metric     string    `json:"metric"`
	Threshold  float64   `json:"threshold"`
	Comparison string    `json:"comparison"` // gt | lt
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	// Evaluated on read, never stored.
	State        string   `json:"state"` // ok | alarm | no_data
	CurrentValue *float64 `json:"current_value,omitempty"`
}

type alarmStore struct {
	mu     sync.Mutex
	alarms map[string]*Alarm
	order  []string
}

func newAlarmStore() *alarmStore { return &alarmStore{alarms: map[string]*Alarm{}} }

func (a *alarmStore) list(families []metricFamily) []*Alarm {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []*Alarm{}
	for _, id := range a.order {
		al, ok := a.alarms[id]
		if !ok {
			continue
		}
		cp := *al
		if v, ok := metricValue(families, al.Metric); ok {
			cp.CurrentValue = &v
			fires := (al.Comparison == "gt" && v > al.Threshold) || (al.Comparison == "lt" && v < al.Threshold)
			if fires {
				cp.State = "alarm"
			} else {
				cp.State = "ok"
			}
		} else {
			cp.State = "no_data"
		}
		out = append(out, &cp)
	}
	return out
}

func (a *alarmStore) create(al *Alarm) *Alarm {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.alarms[al.ID] = al
	a.order = append(a.order, al.ID)
	cp := *al
	return &cp
}

func (a *alarmStore) delete(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.alarms[id]; !ok {
		return false
	}
	delete(a.alarms, id)
	for i, oid := range a.order {
		if oid == id {
			a.order = append(a.order[:i], a.order[i+1:]...)
			break
		}
	}
	return true
}

func (s *Server) alarms() *alarmStore {
	if s.Alarms == nil {
		s.Alarms = newAlarmStore()
	}
	return s.Alarms
}

type alarmRequest struct {
	Name       string  `json:"name"`
	Metric     string  `json:"metric"`
	Threshold  float64 `json:"threshold"`
	Comparison string  `json:"comparison"`
}

func (s *Server) handleCreateAlarm(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var req alarmRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if strings.TrimSpace(req.Metric) == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "metric is required (a Prometheus metric name, e.g. cloudforge_network_engine_up)")
		return
	}
	if req.Comparison != "gt" && req.Comparison != "lt" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "comparison must be gt or lt")
		return
	}
	// The metric must exist right now: an alarm on a name that has
	// never been observed would sit in no_data forever with no
	// explanation. (A metric that stops being observed later still
	// shows no_data honestly.)
	families := gatherMetrics()
	if _, ok := metricValue(families, req.Metric); !ok {
		known := []string{}
		for _, f := range families {
			known = append(known, f.Name)
		}
		s.writeError(w, r, http.StatusBadRequest, "unknown_metric",
			"metric "+req.Metric+" has never been observed; known metrics: "+strings.Join(known, ", "))
		return
	}
	al := s.alarms().create(&Alarm{
		ID: "alarm-" + shortSuffix(newRouteID()), Name: req.Name, Metric: req.Metric,
		Threshold: req.Threshold, Comparison: req.Comparison,
		CreatedBy: u.ID, CreatedAt: time.Now().UTC(),
	})
	s.audit(r, u.ID, "monitoring_alarm_created", "alarm", al.ID, map[string]any{"metric": al.Metric, "threshold": al.Threshold})
	s.writeJSON(w, r, http.StatusCreated, al)
}

func (s *Server) handleDeleteAlarm(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if !s.alarms().delete(id) {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no alarm with that id")
		return
	}
	s.audit(r, u.ID, "monitoring_alarm_deleted", "alarm", id, nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": id})
}

// --- Audit: filtered list + export ----------------------------------------

func parseAuditFilter(r *http.Request) (store.AuditFilter, error) {
	limit, offset := parsePagination(r)
	f := store.AuditFilter{
		ActorID: r.URL.Query().Get("actor_id"), Action: r.URL.Query().Get("action"),
		ResourceType: r.URL.Query().Get("resource_type"), ResourceID: r.URL.Query().Get("resource_id"),
		LabID: r.URL.Query().Get("lab_id"), Q: r.URL.Query().Get("q"),
		Limit: limit, Offset: offset,
	}
	if v := r.URL.Query().Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return f, fmt.Errorf("from must be RFC3339 (e.g. 2026-11-22T00:00:00Z)")
		}
		f.From = t
	}
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return f, fmt.Errorf("to must be RFC3339 (e.g. 2026-11-22T23:59:59Z)")
		}
		f.To = t
	}
	return f, nil
}

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	f, err := parseAuditFilter(r)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	entries, total, err := s.Audit.ListFiltered(r.Context(), f)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if entries == nil {
		entries = []*store.AuditEntry{}
	}
	_ = admin
	s.writeJSON(w, r, http.StatusOK, map[string]any{"audit": entries, "total": total, "limit": f.Limit, "offset": f.Offset})
}

// handleAuditExport streams the filtered audit log as CSV or JSON.
// At most 1000 events per export (the store cap); the response
// carries the total so the caller knows when the export is partial.
func (s *Server) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "format must be csv or json")
		return
	}
	f, err := parseAuditFilter(r)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if f.Limit > 1000 {
		f.Limit = 1000
	}
	entries, total, err := s.Audit.ListFiltered(r.Context(), f)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, admin.ID, "audit_exported", "audit", "", map[string]any{"format": format, "events": len(entries), "total": total})
	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="cloudforge-audit.json"`)
		_ = json.NewEncoder(w).Encode(map[string]any{"events": entries, "total": total, "exported": len(entries)})
	default:
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="cloudforge-audit.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"id", "created_at", "actor_id", "action", "resource_type", "resource_id", "request_id"})
		for _, e := range entries {
			_ = cw.Write([]string{e.ID, e.CreatedAt.Format(time.RFC3339), e.ActorID, e.Action, e.ResourceType, e.ResourceID, e.RequestID})
		}
		cw.Flush()
	}
}
