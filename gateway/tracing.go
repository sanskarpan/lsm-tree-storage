package gateway

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

const tracerName = "lsm-storage/gateway"

// TracingMiddleware wraps an HTTP handler with OpenTelemetry span creation.
// It extracts incoming trace context from W3C TraceContext headers and
// creates a child span for each request.
func TracingMiddleware(next http.Handler) http.Handler {
	tracer := otel.Tracer(tracerName)
	propagator := otel.GetTextMapPropagator()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		spanName := r.Method + " " + r.URL.Path
		ctx, span := tracer.Start(ctx, spanName)
		defer span.End()
		span.SetAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.path", r.URL.Path),
			attribute.String("http.remote_addr", r.RemoteAddr),
		)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
