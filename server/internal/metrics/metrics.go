// Package metrics exposes a zero-dependency Prometheus text-format
// metrics endpoint. StarNexus intentionally avoids the official client
// library to keep the server a single static binary with no C
// dependencies and no transitive supply-chain surface.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MaxLabelsPerMetric caps the number of distinct label combinations a
// single metric may accumulate. Without this cap, a buggy caller that
// emits unbounded labels (e.g. per-request IDs in a label value) would
// exhaust server memory. Prometheus best practice recommends keeping
// per-metric cardinality under a few thousand; we pick a conservative
// 10k ceiling that comfortably handles realistic VPS fleet sizes.
const MaxLabelsPerMetric = 10000

// Registry is a concurrent-safe metrics registry. It holds counters,
// gauges, and summary-style sum/count pairs. Write emits the
// Prometheus text exposition format.
type Registry struct {
	mu            sync.RWMutex
	counters      map[string]*vector
	gauges        map[string]*vector
	summary       map[string]*summaryVector
	help          map[string]string
	typeOf        map[string]string
	droppedLabels map[string]uint64
}

type vector struct {
	samples map[string]*float64Sample
}

type float64Sample struct {
	labels map[string]string
	value  uint64 // float bits for atomic ops (CAS-free updates via Add use sum/count)
}

type summaryVector struct {
	samples map[string]*summarySample
}

type summarySample struct {
	labels map[string]string
	sum    uint64
	count  uint64
}

// New builds an empty registry.
func New() *Registry {
	return &Registry{
		counters:      map[string]*vector{},
		gauges:        map[string]*vector{},
		summary:       map[string]*summaryVector{},
		help:          map[string]string{},
		typeOf:        map[string]string{},
		droppedLabels: map[string]uint64{},
	}
}

// RegisterCounter records metadata for a counter so Write produces
// HELP/TYPE lines. Safe to call repeatedly.
func (r *Registry) RegisterCounter(name, help string) {
	r.mu.Lock()
	r.help[name] = help
	r.typeOf[name] = "counter"
	if _, ok := r.counters[name]; !ok {
		r.counters[name] = &vector{samples: map[string]*float64Sample{}}
	}
	r.mu.Unlock()
}

// RegisterGauge records metadata for a gauge.
func (r *Registry) RegisterGauge(name, help string) {
	r.mu.Lock()
	r.help[name] = help
	r.typeOf[name] = "gauge"
	if _, ok := r.gauges[name]; !ok {
		r.gauges[name] = &vector{samples: map[string]*float64Sample{}}
	}
	r.mu.Unlock()
}

// RegisterSummary records metadata for a summary-style metric
// exposed as name_sum and name_count pairs.
func (r *Registry) RegisterSummary(name, help string) {
	r.mu.Lock()
	r.help[name] = help
	r.typeOf[name] = "summary"
	if _, ok := r.summary[name]; !ok {
		r.summary[name] = &summaryVector{samples: map[string]*summarySample{}}
	}
	r.mu.Unlock()
}

// IncCounter adds 1 to a labelled counter, creating it on first use.
func (r *Registry) IncCounter(name string, labels map[string]string) {
	r.AddCounter(name, labels, 1)
}

// AddCounter increments a counter by delta. Once a metric has
// accumulated MaxLabelsPerMetric distinct label combinations, further
// unseen combos are dropped (counted via droppedLabels) so the
// registry cannot be driven into unbounded memory growth by a
// misbehaving caller.
func (r *Registry) AddCounter(name string, labels map[string]string, delta float64) {
	r.mu.Lock()
	vec, ok := r.counters[name]
	if !ok {
		vec = &vector{samples: map[string]*float64Sample{}}
		r.counters[name] = vec
		if _, exists := r.typeOf[name]; !exists {
			r.typeOf[name] = "counter"
		}
	}
	key := labelKey(labels)
	sample, ok := vec.samples[key]
	if !ok {
		if len(vec.samples) >= MaxLabelsPerMetric {
			r.droppedLabels[name]++
			r.mu.Unlock()
			return
		}
		sample = &float64Sample{labels: cloneLabels(labels)}
		vec.samples[key] = sample
	}
	r.mu.Unlock()
	atomicAddFloat64Bits(&sample.value, delta)
}

// SetGauge writes an absolute gauge value. See AddCounter for the
// cardinality-cap rationale.
func (r *Registry) SetGauge(name string, labels map[string]string, value float64) {
	r.mu.Lock()
	vec, ok := r.gauges[name]
	if !ok {
		vec = &vector{samples: map[string]*float64Sample{}}
		r.gauges[name] = vec
		if _, exists := r.typeOf[name]; !exists {
			r.typeOf[name] = "gauge"
		}
	}
	key := labelKey(labels)
	sample, ok := vec.samples[key]
	if !ok {
		if len(vec.samples) >= MaxLabelsPerMetric {
			r.droppedLabels[name]++
			r.mu.Unlock()
			return
		}
		sample = &float64Sample{labels: cloneLabels(labels)}
		vec.samples[key] = sample
	}
	r.mu.Unlock()
	atomicStoreFloat64Bits(&sample.value, value)
}

// ObserveSummary records a value against a summary-style metric. See
// AddCounter for the cardinality-cap rationale.
func (r *Registry) ObserveSummary(name string, labels map[string]string, value float64) {
	r.mu.Lock()
	vec, ok := r.summary[name]
	if !ok {
		vec = &summaryVector{samples: map[string]*summarySample{}}
		r.summary[name] = vec
		if _, exists := r.typeOf[name]; !exists {
			r.typeOf[name] = "summary"
		}
	}
	key := labelKey(labels)
	sample, ok := vec.samples[key]
	if !ok {
		if len(vec.samples) >= MaxLabelsPerMetric {
			r.droppedLabels[name]++
			r.mu.Unlock()
			return
		}
		sample = &summarySample{labels: cloneLabels(labels)}
		vec.samples[key] = sample
	}
	r.mu.Unlock()
	atomicAddFloat64Bits(&sample.sum, value)
	atomic.AddUint64(&sample.count, 1)
}

// DroppedLabelsFor returns how many times a new label combination was
// refused for a given metric name because the cardinality cap was hit.
// Primarily useful in tests.
func (r *Registry) DroppedLabelsFor(name string) uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.droppedLabels[name]
}

// Handler returns an http.Handler that writes the current registry
// state in Prometheus text format. It is safe to serve directly.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		if err := r.Write(w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}

// Write emits the Prometheus text exposition format.
func (r *Registry) Write(w io.Writer) error {
	r.mu.RLock()
	names := make([]string, 0, len(r.typeOf))
	for name := range r.typeOf {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		help := r.help[name]
		kind := r.typeOf[name]
		if help != "" {
			if _, err := fmt.Fprintf(w, "# HELP %s %s\n", name, escapeHelp(help)); err != nil {
				r.mu.RUnlock()
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "# TYPE %s %s\n", name, kind); err != nil {
			r.mu.RUnlock()
			return err
		}
		switch kind {
		case "counter":
			if err := writeFloatVector(w, name, r.counters[name]); err != nil {
				r.mu.RUnlock()
				return err
			}
		case "gauge":
			if err := writeFloatVector(w, name, r.gauges[name]); err != nil {
				r.mu.RUnlock()
				return err
			}
		case "summary":
			if err := writeSummaryVector(w, name, r.summary[name]); err != nil {
				r.mu.RUnlock()
				return err
			}
		}
	}
	r.mu.RUnlock()
	return nil
}

func writeFloatVector(w io.Writer, name string, vec *vector) error {
	if vec == nil {
		return nil
	}
	keys := make([]string, 0, len(vec.samples))
	for key := range vec.samples {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		sample := vec.samples[key]
		value := atomicLoadFloat64Bits(&sample.value)
		if _, err := fmt.Fprintf(w, "%s%s %s\n", name, formatLabels(sample.labels), formatFloat(value)); err != nil {
			return err
		}
	}
	return nil
}

func writeSummaryVector(w io.Writer, name string, vec *summaryVector) error {
	if vec == nil {
		return nil
	}
	keys := make([]string, 0, len(vec.samples))
	for key := range vec.samples {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		sample := vec.samples[key]
		sum := atomicLoadFloat64Bits(&sample.sum)
		count := atomic.LoadUint64(&sample.count)
		if _, err := fmt.Fprintf(w, "%s_sum%s %s\n", name, formatLabels(sample.labels), formatFloat(sum)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s_count%s %d\n", name, formatLabels(sample.labels), count); err != nil {
			return err
		}
	}
	return nil
}

func labelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(labels[key])
		builder.WriteByte(';')
	}
	return builder.String()
}

func cloneLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	copied := make(map[string]string, len(labels))
	for key, value := range labels {
		copied[key] = value
	}
	return copied
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	builder.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(key)
		builder.WriteString(`="`)
		builder.WriteString(escapeLabelValue(labels[key]))
		builder.WriteByte('"')
	}
	builder.WriteByte('}')
	return builder.String()
}

func escapeLabelValue(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return replacer.Replace(value)
}

func escapeHelp(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	return replacer.Replace(value)
}

func formatFloat(value float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.9f", value), "0"), ".")
}

// --- atomic float helpers ---

func atomicAddFloat64Bits(target *uint64, delta float64) {
	for {
		oldBits := atomic.LoadUint64(target)
		newValue := bitsToFloat(oldBits) + delta
		if atomic.CompareAndSwapUint64(target, oldBits, floatToBits(newValue)) {
			return
		}
	}
}

func atomicStoreFloat64Bits(target *uint64, value float64) {
	atomic.StoreUint64(target, floatToBits(value))
}

func atomicLoadFloat64Bits(target *uint64) float64 {
	return bitsToFloat(atomic.LoadUint64(target))
}

func bitsToFloat(bits uint64) float64 { return float64frombits(bits) }
func floatToBits(value float64) uint64 { return float64bits(value) }

// ---- HTTP middleware ----

// HTTPMiddleware wraps an http.Handler and records request counts and
// latencies. The handler still serves the response; metrics recording
// is zero-copy on the response body.
func (r *Registry) HTTPMiddleware(next http.Handler) http.Handler {
	r.RegisterCounter("starnexus_http_requests_total", "Total HTTP requests by method, normalized path, and status.")
	r.RegisterSummary("starnexus_http_request_seconds", "HTTP request duration in seconds by method and normalized path.")
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(recorder, req)
		elapsed := time.Since(start).Seconds()
		path := normalizePath(req.URL.Path)
		labels := map[string]string{
			"method": req.Method,
			"path":   path,
			"status": statusClass(recorder.status),
		}
		r.IncCounter("starnexus_http_requests_total", labels)
		summaryLabels := map[string]string{
			"method": req.Method,
			"path":   path,
		}
		r.ObserveSummary("starnexus_http_request_seconds", summaryLabels, elapsed)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Path normalization: collapse id-like segments so the cardinality of
// the path label stays bounded. `/api/nodes/tokyo-dmit/details` →
// `/api/nodes/{id}/details`.
var idSegmentRegex = regexp.MustCompile(`^[0-9]+$|^[a-z0-9][a-z0-9-]{2,}$`)

func normalizePath(path string) string {
	if path == "" || path == "/" {
		return "/"
	}
	if !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/download/") && path != "/metrics" {
		return "/static"
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		if i < 2 {
			continue
		}
		if idSegmentRegex.MatchString(part) && part != "ack" && part != "suppress" && part != "details" && part != "recovered" {
			parts[i] = "{id}"
		}
	}
	return "/" + strings.Join(parts, "/")
}

func statusClass(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code >= 400:
		return "4xx"
	case code >= 300:
		return "3xx"
	case code >= 200:
		return "2xx"
	default:
		return "1xx"
	}
}
