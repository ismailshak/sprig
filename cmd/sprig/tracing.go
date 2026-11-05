package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// exportTimeout is how long one export request to the OTLP endpoint may take.
const exportTimeout = 10 * time.Second

// newTracerProvider returns the provider every span in the process is started
// from, and a function that exports the spans still queued and stops it. With
// no endpoint it returns a no-op provider and builds no exporter.
//
// Every setting is passed as an option, because the SDK otherwise reads it from
// an OTEL_* variable and sprig reads only SPRIG_* variables. No option stops
// the SDK merging OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES into the
// resource. The service name and version set here override them. Any other
// attribute in OTEL_RESOURCE_ATTRIBUTES is added to every span.
func newTracerProvider(ctx context.Context, cfg tracingConfig, version string) (trace.TracerProvider, func(context.Context) error, error) {
	if cfg.endpoint == "" {
		return noop.NewTracerProvider(), func(context.Context) error { return nil }, nil
	}

	// The TLS config replaces the one the exporter builds from
	// OTEL_EXPORTER_OTLP_CERTIFICATE and OTEL_EXPORTER_OTLP_CLIENT_*. It is nil
	// for an http endpoint, because the exporter refuses to start with an http
	// endpoint and a TLS config.
	var tlsConfig *tls.Config
	if endpoint, err := url.Parse(cfg.endpoint); err == nil && endpoint.Scheme == "https" {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	options := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(cfg.endpoint),
		// The map is passed even when empty, so OTEL_EXPORTER_OTLP_HEADERS
		// cannot add headers.
		otlptracehttp.WithHeaders(cfg.headers),
		otlptracehttp.WithEncoding(otlptracehttp.EncodingProtobuf),
		otlptracehttp.WithCompression(otlptracehttp.NoCompression),
		otlptracehttp.WithTimeout(exportTimeout),
		otlptracehttp.WithTLSClientConfig(tlsConfig),
	}
	exporter, err := otlptracehttp.New(ctx, options...)
	if err != nil {
		return nil, nil, fmt.Errorf("build the trace exporter: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithMaxQueueSize(sdktrace.DefaultMaxQueueSize),
			sdktrace.WithMaxExportBatchSize(sdktrace.DefaultMaxExportBatchSize),
			sdktrace.WithBatchTimeout(sdktrace.DefaultScheduleDelay*time.Millisecond),
			sdktrace.WithExportTimeout(sdktrace.DefaultExportTimeout*time.Millisecond),
		),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.sampleRatio)),
		sdktrace.WithRawSpanLimits(sdktrace.SpanLimits{
			AttributeValueLengthLimit:   sdktrace.DefaultAttributeValueLengthLimit,
			AttributeValueDepthLimit:    sdktrace.DefaultAttributeValueDepthLimit,
			AttributeCountLimit:         sdktrace.DefaultAttributeCountLimit,
			EventCountLimit:             sdktrace.DefaultEventCountLimit,
			LinkCountLimit:              sdktrace.DefaultLinkCountLimit,
			AttributePerEventCountLimit: sdktrace.DefaultAttributePerEventCountLimit,
			AttributePerLinkCountLimit:  sdktrace.DefaultAttributePerLinkCountLimit,
		}),
		sdktrace.WithResource(resource.NewSchemaless(
			semconv.ServiceName("sprig"),
			semconv.ServiceVersion(version),
		)),
	)
	return provider, provider.Shutdown, nil
}

// logTraceErrors sends the errors the OpenTelemetry SDK reports, such as a
// failed export, to logger at warn level. The handler is global to the
// process. It is the one OpenTelemetry global sprig sets.
func logTraceErrors(logger *slog.Logger) {
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Warn("tracing failed", "err", err)
	}))
}

// tracingShutdownTimeout is how long the spans still queued at exit get to export.
const tracingShutdownTimeout = 5 * time.Second

// stopTracing exports the spans still queued and stops the provider. It runs
// after the process's context is cancelled, so it has a timeout of its own.
func stopTracing(logger *slog.Logger, shutdown func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), tracingShutdownTimeout)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		logger.Warn("tracing did not stop cleanly", "err", err)
	}
}

// inSpan calls f inside a span named name and returns what f returns. An error
// sets the span's status to Error.
func inSpan(ctx context.Context, provider trace.TracerProvider, name string, f func(context.Context) error) error {
	ctx, span := provider.Tracer("github.com/ismailshak/sprig/cmd/sprig").Start(ctx, name)
	defer span.End()
	err := f(ctx)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}
