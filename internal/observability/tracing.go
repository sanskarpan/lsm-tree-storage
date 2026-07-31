// Package observability provides metrics, logging, and tracing for the LSM engine.
package observability

import (
	"context"
	"log"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// InitTracer sets up the global OTLP trace exporter.
// Returns a shutdown function that must be called on exit.
// If OTEL_EXPORTER_OTLP_ENDPOINT is not set, tracing is disabled (no-op).
func InitTracer(serviceName, serviceVersion string) (shutdown func(context.Context) error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		log.Println("tracing: OTEL_EXPORTER_OTLP_ENDPOINT not set; tracing disabled")
		return func(context.Context) error { return nil }
	}

	ctx := context.Background()
	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Printf("tracing: failed to connect to OTLP endpoint %s: %v; tracing disabled", endpoint, err)
		return func(context.Context) error { return nil }
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		log.Printf("tracing: failed to create OTLP exporter: %v; tracing disabled", err)
		return func(context.Context) error { return nil }
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
		semconv.ServiceVersionKey.String(serviceVersion),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	log.Printf("tracing: OTLP exporter connected to %s", endpoint)
	return func(ctx context.Context) error {
		ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return tp.Shutdown(ctxTimeout)
	}
}
