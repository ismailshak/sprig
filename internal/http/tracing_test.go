package http

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// recordSpans returns a provider whose spans are kept in the returned recorder
// once they end.
func recordSpans() (*sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	recorder := tracetest.NewSpanRecorder()
	return sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)), recorder
}

func attributeOf(span sdktrace.ReadOnlySpan, key attribute.Key) (attribute.Value, bool) {
	for _, kv := range span.Attributes() {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// tracedMux returns mux wrapped in Tracing, with MatchPattern outside it to set
// the route pattern Tracing reads.
func tracedMux(mux *http.ServeMux, provider *sdktrace.TracerProvider) http.Handler {
	return MatchPattern(mux)(Tracing(provider)(mux))
}

func TestTracing_ASpanIsNamedByMethodAndRoute(t *testing.T) {
	tests := []struct {
		name, method, path string
		wantName           string
		wantRoute          string
	}{
		{"a matched route", http.MethodGet, "/plants/42", "GET /plants/{plant}", "/plants/{plant}"},
		{"no matched route", http.MethodGet, "/nowhere", "GET", ""},
		{"a method HTTP does not define", "BREW", "/plants/42", "_OTHER /plants/{plant}", "/plants/{plant}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, recorder := recordSpans()
			mux := http.NewServeMux()
			mux.HandleFunc("/plants/{plant}", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			})
			tracedMux(mux, provider).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil))

			spans := recorder.Ended()
			if len(spans) != 1 {
				t.Fatalf("got %d spans, want 1", len(spans))
			}
			span := spans[0]
			if span.Name() != tt.wantName {
				t.Errorf("name = %q, want %q", span.Name(), tt.wantName)
			}
			route, ok := attributeOf(span, "http.route")
			if tt.wantRoute == "" && ok {
				t.Errorf("http.route = %q, want none", route.AsString())
			}
			if tt.wantRoute != "" && route.AsString() != tt.wantRoute {
				t.Errorf("http.route = %q, want %q", route.AsString(), tt.wantRoute)
			}
		})
	}
}

func TestTracing_ASpanRecordsTheMethodAndTheResponseStatus(t *testing.T) {
	provider, recorder := recordSpans()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /plants/{plant}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	tracedMux(mux, provider).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/plants/42", nil))

	span := recorder.Ended()[0]
	if method, _ := attributeOf(span, "http.request.method"); method.AsString() != http.MethodGet {
		t.Errorf("http.request.method = %q, want GET", method.AsString())
	}
	if status, _ := attributeOf(span, "http.response.status_code"); status.AsInt64() != http.StatusTeapot {
		t.Errorf("http.response.status_code = %d, want %d", status.AsInt64(), http.StatusTeapot)
	}
	if span.Status().Code == codes.Error {
		t.Error("a 418 set the span's status to Error")
	}
}

func TestTracing_NoAttributeHoldsTheTokenInAnInviteLink(t *testing.T) {
	const token = "Q2xpZW50U2VjcmV0VG9rZW4" //nolint:gosec // G101 sees a name like a credential, and the value is made up
	provider, recorder := recordSpans()
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+invitedPattern, func(http.ResponseWriter, *http.Request) {})
	tracedMux(mux, provider).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/invite/"+token+"?from="+token, nil))

	span := recorder.Ended()[0]
	if strings.Contains(span.Name(), token) {
		t.Errorf("the span name %q holds the token", span.Name())
	}
	for _, kv := range span.Attributes() {
		if strings.Contains(kv.Value.String(), token) {
			t.Errorf("%s = %q holds the token", kv.Key, kv.Value.String())
		}
	}
}

func TestTracing_A500SetsTheSpanStatusToError(t *testing.T) {
	provider, recorder := recordSpans()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /plants/{plant}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	tracedMux(mux, provider).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/plants/42", nil))

	if got := recorder.Ended()[0].Status().Code; got != codes.Error {
		t.Errorf("span status = %v, want Error", got)
	}
}

func TestTracing_ATraceparentHeaderDoesNotSetTheTraceID(t *testing.T) {
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	provider, recorder := recordSpans()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /plants/{plant}", func(http.ResponseWriter, *http.Request) {})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/plants/42", nil)
	req.Header.Set("traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
	tracedMux(mux, provider).ServeHTTP(httptest.NewRecorder(), req)

	span := recorder.Ended()[0]
	if got := span.SpanContext().TraceID().String(); got == traceID {
		t.Errorf("trace id = %s, the one the traceparent header named", got)
	}
	if span.Parent().IsValid() {
		t.Errorf("the span has parent %s, want none", span.Parent().SpanID())
	}
}

func TestNew_TheHealthCheckGetsNoSpan(t *testing.T) {
	provider, recorder := recordSpans()
	deps := testDependencies(t)
	deps.TracerProvider = provider
	New(deps).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))

	if spans := recorder.Ended(); len(spans) != 0 {
		t.Errorf("got %d spans, want none: the first is %q", len(spans), spans[0].Name())
	}
}

// The request line has the trace id only because New puts Tracing outside
// Logging.
func TestNew_TheRequestLogLineHasTheSpansTraceID(t *testing.T) {
	provider, recorder := recordSpans()
	var buf bytes.Buffer
	deps := testDependencies(t)
	deps.Logger = slog.New(NewContextHandler(slog.NewJSONHandler(&buf, nil)))
	deps.TracerProvider = provider
	New(deps).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, offlinePath, nil))

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("the log was not one JSON line: %v\n%s", err, buf.String())
	}
	if want := spans[0].SpanContext().TraceID().String(); entry["trace_id"] != want {
		t.Errorf("trace_id = %v, want %s", entry["trace_id"], want)
	}
}

func TestNewContextHandler_ALineLoggedInsideASpanHasItsTraceAndSpanIDs(t *testing.T) {
	provider, _ := recordSpans()
	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewJSONHandler(&buf, nil)))
	ctx, span := provider.Tracer("test").Start(t.Context(), "request")
	logger.InfoContext(ctx, "plant saved")
	span.End()

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("the log was not one JSON line: %v\n%s", err, buf.String())
	}
	if want := span.SpanContext().TraceID().String(); entry["trace_id"] != want {
		t.Errorf("trace_id = %v, want %s", entry["trace_id"], want)
	}
	if want := span.SpanContext().SpanID().String(); entry["span_id"] != want {
		t.Errorf("span_id = %v, want %s", entry["span_id"], want)
	}
}

func TestNewContextHandler_ALineLoggedOutsideASpanHasNoTraceID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewJSONHandler(&buf, nil)))
	logger.InfoContext(t.Context(), "digest sent")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("the log was not one JSON line: %v\n%s", err, buf.String())
	}
	if _, ok := entry["trace_id"]; ok {
		t.Errorf("trace_id = %v, want none", entry["trace_id"])
	}
}
