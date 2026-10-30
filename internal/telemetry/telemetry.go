// Package telemetry menyiapkan OpenTelemetry tracing untuk backend.
package telemetry

import (
	"context"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/whoisrry/cobapram/internal/config"
)

// Setup memasang TracerProvider global yang mengekspor span lewat OTLP/HTTP.
// Endpoint dibaca SDK dari OTEL_EXPORTER_OTLP_ENDPOINT (misalnya
// http://jaeger:4318). Jika OTEL_ENABLED=false, provider global tetap no-op
// sehingga instrumentasi tidak berbiaya.
//
// Fungsi shutdown yang dikembalikan wajib dipanggil saat server berhenti
// agar span yang masih di buffer terkirim.
func Setup(ctx context.Context, cfg config.Config) (shutdown func(context.Context) error, err error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	if !cfg.OTelEnabled {
		return func(context.Context) error { return nil }, nil
	}

	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			attribute.String("service.name", cfg.OTelServiceName),
			attribute.String("deployment.environment.name", cfg.Env),
		),
	)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// Traceparent mengembalikan header W3C traceparent dari span aktif. Nilai ini
// dirender ke <meta name="traceparent"> agar span document-load di browser
// menjadi child dari span server yang merender halaman tersebut.
func Traceparent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

// Route memberi nama span server sesuai pola route (misalnya
// "GET /tickets/{id}") alih-alih path mentah yang berisi ID, agar nama span
// tidak meledak jumlahnya dan mudah dicari di Jaeger.
func Route(pattern string, next http.Handler) http.Handler {
	route := pattern
	if _, path, ok := strings.Cut(pattern, " "); ok {
		route = path
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		span.SetName(pattern)
		span.SetAttributes(attribute.String("http.route", route))
		next.ServeHTTP(w, r)
	})
}

// RecordError menandai span aktif sebagai error.
func RecordError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// TraceID mengembalikan trace id span aktif (kosong jika tidak ada), untuk
// korelasi log dengan trace di Jaeger.
func TraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}
