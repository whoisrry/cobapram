// Telemetri browser (OpenTelemetry). Dibundel oleh esbuild menjadi
// /static/otel.js. Aplikasi tetap MPA tanpa framework; file ini hanya
// mengirim span, tidak mengubah perilaku halaman.
//
// Span yang dihasilkan:
//   - documentLoad / documentFetch / resourceFetch: waktu muat setiap halaman.
//     documentLoad menjadi child dari span server lewat <meta name="traceparent">,
//     sehingga satu trace di Jaeger berisi server + browser.
//   - form.submit: setiap form dikirim (nama span memakai pola route, bukan ID).
import { context, propagation, trace } from "@opentelemetry/api";
import { W3CTraceContextPropagator } from "@opentelemetry/core";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-http";
import { registerInstrumentations } from "@opentelemetry/instrumentation";
import { DocumentLoadInstrumentation } from "@opentelemetry/instrumentation-document-load";
import { resourceFromAttributes } from "@opentelemetry/resources";
import { BatchSpanProcessor, StackContextManager, TracerProvider } from "@opentelemetry/sdk-trace";

const provider = new TracerProvider({
  resource: resourceFromAttributes({ "service.name": "ticketing-web" }),
  spanProcessors: [
    // Versi browser melakukan flush saat tab disembunyikan/ditutup, penting
    // untuk MPA karena setiap navigasi menutup halaman.
    // sdk-trace 3.x: exporter diberikan di dalam objek options.
    new BatchSpanProcessor({
      // Same-origin: Caddy meneruskan /otel/v1/traces ke Jaeger (OTLP/HTTP).
      exporter: new OTLPTraceExporter({ url: new URL("/otel/v1/traces", window.location.origin).href }),
      scheduledDelayMillis: 2000,
    }),
  ],
});

trace.setGlobalTracerProvider(provider);
propagation.setGlobalPropagator(new W3CTraceContextPropagator());
context.setGlobalContextManager(new StackContextManager().enable());

registerInstrumentations({
  instrumentations: [new DocumentLoadInstrumentation()],
});

// Ganti angka di path dengan {id} agar nama span konsisten dengan route server.
function routeOf(path) {
  return path.replace(/\/\d+(?=\/|$)/g, "/{id}");
}

const tracer = trace.getTracer("ticketing-web");
document.addEventListener("submit", (event) => {
  const form = event.target;
  if (!(form instanceof HTMLFormElement)) return;
  const action = new URL(form.action, window.location.href);
  const span = tracer.startSpan(`form.submit ${form.method.toUpperCase()} ${routeOf(action.pathname)}`, {
    attributes: {
      "http.request.method": form.method.toUpperCase(),
      "url.path": routeOf(action.pathname),
      "page.path": routeOf(window.location.pathname),
    },
  });
  span.end();
});
