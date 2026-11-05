package main

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// collector is a test HTTP server in place of an OTLP endpoint. It records the
// path and headers of every export it receives.
type collector struct {
	*httptest.Server
	mu       sync.Mutex
	requests []*http.Request
}

func newCollector(t *testing.T) *collector {
	t.Helper()
	c := &collector{}
	c.Server = httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.requests = append(c.requests, r)
	}))
	t.Cleanup(c.Close)
	return c
}

func (c *collector) received() []*http.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests
}

// exportOneSpan builds the provider from cfg, ends one span on it and shuts it
// down. Shutdown exports the spans the provider holds.
func exportOneSpan(t *testing.T, cfg tracingConfig) {
	t.Helper()
	provider, shutdown, err := newTracerProvider(t.Context(), cfg, "v1.2.3")
	if err != nil {
		t.Fatalf("newTracerProvider: %v", err)
	}
	_, span := provider.Tracer("test").Start(t.Context(), "GET /plants")
	span.End()
	if err := shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestNewTracerProvider_OTELExporterVariablesDoNotTurnTracingOn(t *testing.T) {
	collector := newCollector(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", collector.URL+"/v1/traces")
	cfg, err := loadConfig(tracingEnv(map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": collector.URL}))
	if err != nil {
		t.Fatalf("loadConfig returned an error: %v", err)
	}

	exportOneSpan(t, cfg.tracing)

	if got := len(collector.received()); got != 0 {
		t.Errorf("the OTEL_* endpoint received %d exports, want none", got)
	}
}

func TestNewTracerProvider_ExportsToTheSPRIGEndpointWithTheSPRIGHeadersOnly(t *testing.T) {
	collector := newCollector(t)
	elsewhere := newCollector(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", elsewhere.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "X-From-OTEL=1")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "X-From-OTEL-Traces=1")

	exportOneSpan(t, tracingConfig{
		endpoint:    collector.URL + "/otlp/v1/traces",
		headers:     map[string]string{"Authorization": "Bearer abc"},
		sampleRatio: 1,
	})

	requests := collector.received()
	if len(requests) != 1 {
		t.Fatalf("the SPRIG_OTLP_ENDPOINT received %d exports, want 1", len(requests))
	}
	if got := requests[0].URL.Path; got != "/otlp/v1/traces" {
		t.Errorf("the export went to %s, want /otlp/v1/traces", got)
	}
	if got := requests[0].Header.Get("Authorization"); got != "Bearer abc" {
		t.Errorf("Authorization = %q, want the SPRIG_OTLP_HEADERS value", got)
	}
	for _, name := range []string{"X-From-OTEL", "X-From-OTEL-Traces"} {
		if got := requests[0].Header.Get(name); got != "" {
			t.Errorf("%s = %q, want no header from an OTEL_* variable", name, got)
		}
	}
	if got := len(elsewhere.received()); got != 0 {
		t.Errorf("the OTEL_* endpoint received %d exports, want none", got)
	}
}

func TestNewTracerProvider_ASampleRatioOfZeroExportsNothingWhenTheOTELSamplerIsAlwaysOn(t *testing.T) {
	collector := newCollector(t)
	t.Setenv("OTEL_TRACES_SAMPLER", "always_on")

	exportOneSpan(t, tracingConfig{endpoint: collector.URL + "/v1/traces", headers: map[string]string{}, sampleRatio: 0})

	if got := len(collector.received()); got != 0 {
		t.Errorf("the endpoint received %d exports, want none", got)
	}
}

func TestNewTracerProvider_AnHTTPEndpointExportsWhenOTELExporterCertificateIsSet(t *testing.T) {
	collector := newCollector(t)
	tlsServer := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(tlsServer.Close)
	certificate := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsServer.Certificate().Raw}), 0o600); err != nil {
		t.Fatalf("writing the certificate: %v", err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", certificate)

	exportOneSpan(t, tracingConfig{endpoint: collector.URL + "/v1/traces", headers: map[string]string{}, sampleRatio: 1})

	if got := len(collector.received()); got != 1 {
		t.Errorf("the endpoint received %d exports, want 1", got)
	}
}
