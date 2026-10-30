# Changelog

Semua perubahan penting pada proyek ini dicatat di file ini.
Format mengikuti [Keep a Changelog](https://keepachangelog.com/id-ID/1.1.0/).

## [Unreleased]

### Added
- Aplikasi support ticketing MPA (Go `net/http`, `html/template`): login, daftar tiket, buat tiket (client), detail tiket dengan balasan, ubah status (admin), logout.
- Autentikasi JWT HS256 (access token 15 menit, refresh token 7 hari) di cookie `__Host-` httpOnly; rotasi refresh token di Redis dengan deteksi reuse yang mencabut semua sesi user.
- Rate limit login 10 percobaan per 15 menit per IP (Redis), proteksi CSRF (gorilla/csrf) di semua POST, header keamanan (CSP, X-Frame-Options, Referrer-Policy, X-Content-Type-Options).
- Pemeriksaan kepemilikan tiket di klausa `WHERE`; tiket milik orang lain menghasilkan 404.
- Skema PostgreSQL dengan migrasi goose (`users`, `tickets`, `replies`, constraint CHECK, foreign key, index).
- CLI `create-user` (password dari stdin) dan `migrate`.
- Docker multi-stage (distroless, binary statis) dan Docker Compose: app, PostgreSQL, Redis, Caddy, Jaeger.
- Akses lewat alamat IP dengan TLS dari CA internal Caddy (tanpa Let's Encrypt); `CADDY_TLS` untuk beralih ke ACME pada domain publik.
- OpenTelemetry: tracing backend (HTTP per route, GORM, Redis, `trace_id` di log) dan browser (`/static/otel.js`: document load, form submit) yang tergabung dalam satu trace; Jaeger UI di port 16686.
- Makefile: `make env`, `make up`, `make down`, `make restart`, `make user`, `make ca-cert`, `make logs`, `make ps`, `make test`.
- Unit test untuk validasi JWT, rotasi/reuse refresh token, rate limit, dan kepemilikan tiket.
