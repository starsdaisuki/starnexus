package metrics

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestRegistryCounterAndGauge(t *testing.T) {
	r := New()
	r.RegisterCounter("requests_total", "request counter")
	r.RegisterGauge("queue_depth", "queue depth")

	r.IncCounter("requests_total", map[string]string{"method": "GET"})
	r.IncCounter("requests_total", map[string]string{"method": "GET"})
	r.IncCounter("requests_total", map[string]string{"method": "POST"})
	r.SetGauge("queue_depth", map[string]string{"queue": "ingest"}, 12)

	var buf bytes.Buffer
	if err := r.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, `requests_total{method="GET"} 2`) {
		t.Fatalf("expected GET counter to equal 2, got:\n%s", output)
	}
	if !strings.Contains(output, `requests_total{method="POST"} 1`) {
		t.Fatalf("expected POST counter to equal 1, got:\n%s", output)
	}
	if !strings.Contains(output, `queue_depth{queue="ingest"} 12`) {
		t.Fatalf("expected gauge to equal 12, got:\n%s", output)
	}
	if !strings.Contains(output, "# TYPE requests_total counter") {
		t.Fatalf("expected TYPE line for counter")
	}
	if !strings.Contains(output, "# HELP queue_depth queue depth") {
		t.Fatalf("expected HELP line for gauge")
	}
}

func TestRegistrySummaryIsSumAndCount(t *testing.T) {
	r := New()
	r.RegisterSummary("op_seconds", "op duration")
	r.ObserveSummary("op_seconds", map[string]string{"op": "x"}, 0.25)
	r.ObserveSummary("op_seconds", map[string]string{"op": "x"}, 0.75)

	var buf bytes.Buffer
	if err := r.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, `op_seconds_sum{op="x"} 1`) {
		t.Fatalf("expected sum 1.0, got:\n%s", output)
	}
	if !strings.Contains(output, `op_seconds_count{op="x"} 2`) {
		t.Fatalf("expected count 2, got:\n%s", output)
	}
}

func TestHTTPMiddlewareRecordsRequests(t *testing.T) {
	r := New()
	handler := r.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	server := httptest.NewServer(handler)
	defer server.Close()

	for _, path := range []string{"/api/nodes", "/api/nodes/tokyo-dmit/details", "/api/nodes/jp-lisahost/details"} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		resp.Body.Close()
	}

	var buf bytes.Buffer
	if err := r.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, `starnexus_http_requests_total{method="GET",path="/api/nodes",status="2xx"} 1`) {
		t.Fatalf("expected /api/nodes counter, got:\n%s", output)
	}
	if !strings.Contains(output, `starnexus_http_requests_total{method="GET",path="/api/nodes/{id}/details",status="2xx"} 2`) {
		t.Fatalf("expected 2 normalized id-path requests, got:\n%s", output)
	}
}

func TestRegistryCapsCardinality(t *testing.T) {
	r := New()
	r.RegisterCounter("test_requests", "test counter")
	// Emit one more distinct label than the cap; the extra must be
	// dropped silently rather than causing unbounded memory growth.
	for i := 0; i <= MaxLabelsPerMetric; i++ {
		r.IncCounter("test_requests", map[string]string{"id": strconv.Itoa(i)})
	}
	if got := r.DroppedLabelsFor("test_requests"); got != 1 {
		t.Fatalf("expected exactly one dropped label when exceeding cap, got %d", got)
	}
	var buf bytes.Buffer
	if err := r.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	// The accepted-label sample lines should be exactly MaxLabelsPerMetric.
	lines := strings.Count(buf.String(), "test_requests{")
	if lines != MaxLabelsPerMetric {
		t.Fatalf("expected %d sample lines, got %d", MaxLabelsPerMetric, lines)
	}
}

func TestNormalizePath(t *testing.T) {
	cases := []struct {
		input  string
		output string
	}{
		{"/", "/"},
		{"/api/nodes", "/api/nodes"},
		{"/api/nodes/tokyo-dmit", "/api/nodes/{id}"},
		{"/api/nodes/tokyo-dmit/details", "/api/nodes/{id}/details"},
		{"/api/incidents/42/ack", "/api/incidents/{id}/ack"},
		{"/metrics", "/metrics"},
		{"/index.html", "/static"},
	}
	for _, tc := range cases {
		if got := normalizePath(tc.input); got != tc.output {
			t.Errorf("normalizePath(%q) = %q, want %q", tc.input, got, tc.output)
		}
	}
}
