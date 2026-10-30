// Package middleware berisi middleware HTTP umum: header keamanan, logging,
// recover, batas ukuran body, dan autentikasi.
package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/whoisrry/cobapram/internal/telemetry"
)

// Chain menerapkan middleware dengan urutan terluar lebih dulu:
// Chain(h, a, b) == a(b(h)).
func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// SecurityHeaders menambahkan header keamanan dasar. CSP ketat tanpa inline
// script/style. Satu-satunya JavaScript adalah /static/otel.js (telemetri
// browser), jadi script-src dan connect-src 'self' hanya diizinkan jika
// telemetri aktif; span dikirim ke /otel/v1/traces di origin yang sama.
func SecurityHeaders(hsts, browserTelemetry bool) func(http.Handler) http.Handler {
	csp := "default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; " +
		"frame-ancestors 'none'; base-uri 'none'"
	if browserTelemetry {
		csp += "; script-src 'self'; connect-src 'self'"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Frame-Options", "DENY")
			// same-origin (bukan no-referrer): browser mengirim Origin/Referer
			// pada POST same-origin, yang diperiksa oleh gorilla/csrf.
			h.Set("Referrer-Policy", "same-origin")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// MaxBody membatasi ukuran body request (form tiket maksimal ~10 ribu karakter).
func MaxBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Logger mencatat setiap request sebagai log terstruktur.
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			if r.URL.Path == "/healthz" {
				return // hindari banjir log dari health check
			}
			log.LogAttrs(r.Context(), slog.LevelInfo, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Duration("duration", time.Since(start)),
				slog.String("trace_id", telemetry.TraceID(r.Context())),
			)
		})
	}
}

// Recover mengubah panic menjadi 500 dan mencatatnya, tanpa mematikan server.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v)
					}
					log.ErrorContext(r.Context(), "panic", "error", v, "method", r.Method, "path", r.URL.Path)
					telemetry.RecordError(r.Context(), fmt.Errorf("panic: %v", v))
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
