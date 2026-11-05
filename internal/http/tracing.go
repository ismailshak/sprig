package http

import (
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// Tracing starts a server span for every request except the health check,
// named by the method and the route pattern, such as "GET /plants/{plant}".
// A 5xx sets the span's status to Error.
//
// The span never records the request's path or query. An invite link and a
// setup link hold their token in the path, and the route pattern holds
// "{token}" in its place.
//
// The span is always a new trace. A traceparent header on the request is
// ignored, because the server is reachable from the internet and honouring the
// header would let any client choose the trace id and whether it is sampled.
func Tracing(provider trace.TracerProvider) func(http.Handler) http.Handler {
	tracer := provider.Tracer("github.com/ismailshak/sprig/internal/http")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pattern := patternFrom(r)
			if pattern == healthzPattern {
				next.ServeHTTP(w, r)
				return
			}

			method := spanMethod(r.Method)
			name := method
			attrs := []attribute.KeyValue{semconv.HTTPRequestMethodKey.String(method)}
			if route := routeOf(pattern); route != "" {
				name += " " + route
				attrs = append(attrs, semconv.HTTPRoute(route))
			}
			ctx, span := tracer.Start(r.Context(), name,
				trace.WithNewRoot(),
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(attrs...),
			)
			defer span.End()

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r.WithContext(ctx))

			status := responseStatus(rec, r)
			span.SetAttributes(semconv.HTTPResponseStatusCode(status))
			if status >= 500 {
				span.SetStatus(codes.Error, "")
			}
		})
	}
}

// routeOf returns a route pattern with its method removed, so
// "GET /plants/{plant}" is "/plants/{plant}". An empty pattern, from a request
// no route matched, returns an empty route.
func routeOf(pattern string) string {
	if _, route, ok := strings.Cut(pattern, " "); ok {
		return route
	}
	return pattern
}

// spanMethod returns method when it is one of the methods HTTP defines, and
// "_OTHER" otherwise. A client can send any method name, and each distinct
// name would otherwise become a span name of its own.
func spanMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return method
	default:
		return "_OTHER"
	}
}
