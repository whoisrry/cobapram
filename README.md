# cobapram — Support Ticketing

Aplikasi support ticketing ringan: client membuat dan membalas tiket miliknya; admin melihat semua tiket, memfilter, membalas, dan mengubah status. Server-rendered (MPA) dengan Go `net/http` + `html/template`, PostgreSQL (GORM), Redis, JWT, Docker Compose, dan Caddy. Tracing OpenTelemetry untuk backend dan browser, bisa dilihat di Jaeger UI.

## Mulai cepat

```bash
make env     # buat .env: IP server terdeteksi otomatis + semua secret acak
make up      # build, migrasi database, jalankan semua service
make user EMAIL=admin@example.com NAME="Admin" ROLE=admin   # password diminta interaktif

make down && make up   # restart (data tetap tersimpan)
```

- Aplikasi: `https://<IP-server>`. Sertifikat dari CA internal Caddy, **bukan** Let's Encrypt, jadi browser akan memberi peringatan sampai root CA di-import (`make ca-cert`).
- Jaeger UI: `http://<IP-server>:16686`.
- IP terdeteksi salah (misalnya server punya beberapa interface)? Hapus `.env` lalu jalankan `make env IP=10.0.0.5`.
- `make` tanpa argumen menampilkan semua perintah.

Kebutuhan: Docker Engine dengan Compose v2.17+ (untuk `--wait`) dan `make`.

## Keputusan yang berbeda dari spesifikasi / tambahan

| Keputusan | Alasan |
|---|---|
| Go **1.27.3** | Versi stable terbaru saat dibuat. |
| Binary ketiga **`cmd/migrate`** | Image final distroless tidak punya shell maupun CLI goose. Migrasi di-embed ke binary dan dijalankan dengan `migrate up`. |
| **Paginasi 25 tiket/halaman** | "Lihat semua tiket" tanpa batas akan makin lambat seiring data bertambah. |
| **Access token juga dicek ke Redis setiap request** | Logout dan pencabutan sesi langsung berlaku, tidak menunggu access token (15 menit) kedaluwarsa. Biayanya satu `EXISTS` Redis per request. |
| **`COOKIE_SECURE=false`** (hanya development) | Untuk menjalankan binary langsung tanpa HTTPS. Ditolak saat `APP_ENV=production`. |
| **Tidak ada tabel aktivitas** | Skema tidak memuatnya. Transaksi dipakai di operasi yang menulis lebih dari satu tabel: *balasan + `tickets.updated_at`*. Ubah status hanya satu `UPDATE`. Membuat tiket juga satu `INSERT`, karena isi awal tiket ada di `tickets.body`. |
| **Rate limit menghitung semua percobaan** login (berhasil maupun gagal) | Atomik dengan `INCR`, jadi serangan paralel tetap terhitung. Konsekuensinya, banyak user di balik satu IP NAT berbagi kuota 10/15 menit. |
| **Refresh token sliding 7 hari** | Setiap rotasi memperpanjang sesi. Sesi berakhir setelah 7 hari tidak aktif. |
| Unit test memakai **SQLite pure-Go + AutoMigrate** | Test berjalan tanpa Postgres/Docker. AutoMigrate hanya ada di file `_test.go`; skema produksi tetap lewat goose. |
| **Akses lewat IP dengan TLS internal Caddy** (bukan Let's Encrypt) | Sesuai permintaan. HTTPS tetap dipakai, bukan HTTP polos, karena cookie `Secure`/`__Host-` dan pemeriksaan Origin CSRF membutuhkan HTTPS; di HTTP polos login tidak akan berfungsi. Untuk domain publik dengan Let's Encrypt cukup set `CADDY_TLS=email@domain` dan `SITE_ADDRESS=domain`. |
| **Satu file JavaScript: `/static/otel.js`** (OpenTelemetry Web, ~63 KB) | Telemetri browser butuh JS. File ini dibundel dengan esbuild dari `web/`, tanpa framework, dan tidak mengubah perilaku halaman. CSP mengizinkan `script-src 'self'` dan `connect-src 'self'` hanya jika `OTEL_ENABLED=true`. |
| **Endpoint `/otel/v1/traces` di Caddy** | Browser mengirim span ke origin yang sama, lalu Caddy meneruskannya ke Jaeger. Port OTLP Jaeger tidak dibuka ke host. |
| Tag image dikunci ke versi (`postgres:17.6-alpine`, `redis:7.4.5-alpine`, `caddy:2.10.2-alpine`, `jaegertracing/jaeger:2.22.0`, `node:24.22.2-alpine`, `golang:1.27.3-alpine`) | Periksa versi patch terbaru sebelum deploy. Untuk reproducibility penuh, kunci juga ke digest (`image@sha256:...`). |

## Keputusan penting (ringkas)

- **JWT HS256.** Hanya satu service yang menandatangani dan memverifikasi token, jadi kunci asimetris (EdDSA) tidak memberi manfaat. Parser mengunci algoritma ke HS256 (menolak `none`, HS512, RS256, dll.), mewajibkan `exp`, dan memeriksa `iss` serta `aud`. Claim `token_use` mencegah refresh token dipakai sebagai access token, dan sebaliknya.
- **Refresh token rotation + reuse detection** (`internal/auth/session.go`). Rotasi dijalankan oleh skrip Lua di Redis secara atomik: token lama dihapus dan ditandai `rt_used:{jti}`, lalu token baru disimpan. Jika token yang sudah ditandai dipakai lagi, **semua sesi user dicabut**. Jika dua request memakai token yang sama secara bersamaan, hanya satu yang berhasil.
- **Refresh transparan.** Karena MPA (JavaScript hanya untuk telemetri), middleware `RequireAuth` merotasi refresh token sendiri saat access token kedaluwarsa, lalu memasang cookie baru pada response yang sama.
- **Cookie** `__Host-access` / `__Host-refresh`: `HttpOnly`, `Secure`, `SameSite=Lax`. Lax dipilih (bukan Strict) agar tautan tiket yang dibuka dari luar aplikasi tetap dalam keadaan login. Serangan CSRF ditangani token gorilla/csrf di semua POST, termasuk login dan logout.
- **Kepemilikan di klausa WHERE** (`internal/ticket/ticket.go`). Semua query tiket client memakai `WHERE tickets.user_id = ?`. Tiket orang lain menghasilkan `ErrNotFound` yang sama dengan tiket yang tidak ada, sehingga responsnya **404**. Balasan memakai `UPDATE ... WHERE id = ? AND user_id = ?` di dalam transaksi; jika 0 baris terdampak, tidak ada data yang ditulis.
- **Tanpa N+1.** Detail tiket selalu 3 query (tiket JOIN pemilik, balasan, penulis balasan), berapa pun jumlah balasannya. Daftar tiket 1 query (`Joins("User")`) tanpa kolom `body`. Kolom user yang dimuat hanya `id, name, role`, jadi `password_hash` tidak ikut terambil.
- **Setiap query** memakai `WithContext` dengan timeout `DB_QUERY_TIMEOUT`. Connection pool diatur dari `DB_MAX_OPEN_CONNS`, `DB_MAX_IDLE_CONNS`, dan `DB_CONN_MAX_LIFETIME`.
- **Tidak memakai soft delete.** Dengan `gorm.DeletedAt`, GORM otomatis menambahkan `WHERE deleted_at IS NULL` ke setiap query model tersebut, termasuk Preload dan Joins. Akibatnya, index perlu mencakup kondisi itu (partial index `WHERE deleted_at IS NULL`), constraint `UNIQUE(email)` akan bentrok dengan baris yang sudah "dihapus", dan `ON DELETE CASCADE` tidak pernah terpicu karena baris tidak benar-benar dihapus. Karena aplikasi ini tidak punya fitur hapus, soft delete tidak dipakai.
- **Login** memakai pesan generik "Email atau password salah". Saat email tidak ditemukan, bcrypt tetap dijalankan terhadap hash dummy agar waktu respons sama (mencegah enumerasi email lewat timing).
- **Header keamanan**: CSP ketat (`default-src 'none'`, CSS hanya dari `/static`, tanpa inline script/style; `script-src`/`connect-src 'self'` hanya jika telemetri aktif), `X-Frame-Options: DENY`, `Referrer-Policy: same-origin`, `X-Content-Type-Options: nosniff`, dan HSTS di production (browser mengabaikan HSTS untuk alamat IP dan untuk koneksi dengan sertifikat yang belum dipercaya, jadi tidak mengunci akses lewat IP).

## OpenTelemetry & Jaeger

```
browser ──/otel/v1/traces──► Caddy ──► jaeger:4318 (OTLP/HTTP) ◄── app (OTLP/HTTP)
                                         │
                                Jaeger UI 0.0.0.0:16686
```

**Backend (service `ticketing-app`)**, satu trace per request:
- span HTTP server bernama sesuai pola route (`GET /tickets/{id}`, `POST /tickets/{id}/replies`, ...), bukan path mentah berisi ID;
- span per query GORM (`select tickets`, `insert replies`, ...); hanya SQL berparameter yang dicatat, **tanpa nilai parameter**;
- span per perintah Redis (`exists`, `evalsha`, pipeline); hanya nama perintah, **tanpa argumen** (yang berisi id sesi/jti);
- error 500 dan panic ditandai sebagai error pada span;
- setiap baris log request berisi `trace_id`, jadi log bisa dicocokkan dengan trace di Jaeger;
- `/healthz` dan `/static/*` tidak di-trace.

**Browser (service `ticketing-web`)**:
- `documentLoad` → `documentFetch` + `resourceFetch` (CSS/JS): waktu muat setiap halaman;
- `form.submit <METHOD> <route>` untuk setiap form yang dikirim;
- server merender `<meta name="traceparent">`, sehingga `documentLoad` menjadi **child dari span server** yang merender halaman itu. Hasilnya, satu trace di Jaeger memperlihatkan server + database + Redis + waktu muat di browser.

Konfigurasi: `OTEL_ENABLED` (true/false) dan `OTEL_SERVICE_NAME` di `.env`. Endpoint (`OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4318`) ditetapkan di compose, dan variabel standar `OTEL_*` lain (misalnya `OTEL_TRACES_SAMPLER`) dibaca langsung oleh SDK Go.

Jaeger memakai config sendiri (`jaeger.yaml`): penyimpanan **memori** (maksimal 50.000 trace, hilang saat container Jaeger restart), UI listen di `0.0.0.0:16686`, OTLP di `0.0.0.0:4317/4318` (hanya di jaringan Docker).

> **Perhatian keamanan:** Jaeger UI **tidak memiliki login**. Trace berisi path URL, IP client, user agent, dan SQL (tanpa nilai). Batasi port 16686 hanya untuk jaringan private, misalnya:
> ```bash
> ufw allow from 10.0.0.0/8 to any port 16686 proto tcp
> ```
> Karena Docker mem-bypass ufw untuk port yang di-publish, cara yang lebih pasti adalah memakai firewall di level jaringan/cloud (security group), atau mengganti `"0.0.0.0:16686:16686"` dengan `"<IP-private>:16686:16686"` di `docker-compose.yml`.
>
> Endpoint `/otel/v1/traces` bisa menerima span dari siapa pun yang menjangkau aplikasi (dibatasi POST dan 512 KB per request). Untuk aplikasi yang dibuka ke internet, pertimbangkan membatasinya, atau matikan telemetri browser dengan `OTEL_ENABLED=false`.

## Struktur folder

```
.
├── cmd/
│   ├── server/          # web server (+ subcommand "healthcheck" untuk Docker)
│   ├── create-user/     # CLI pembuatan akun
│   └── migrate/         # menjalankan migrasi goose yang di-embed
├── internal/
│   ├── config/          # semua konfigurasi dari environment variable
│   ├── db/              # koneksi GORM + connection pool
│   ├── models/          # struct GORM: User, Ticket, Reply
│   ├── auth/            # JWT, sesi Redis (rotasi/reuse), rate limit, bcrypt, cookie
│   ├── ticket/          # query tiket dengan pemeriksaan kepemilikan
│   ├── middleware/      # header keamanan, logging slog, recover, RequireAuth/RequireRole
│   ├── handler/         # handler HTTP + routing net/http Go 1.22+
│   ├── telemetry/       # setup OpenTelemetry, nama span per route, traceparent
│   └── templates/       # html/template + static/ (di-embed; otel.js hasil build web/)
├── migrations/          # file SQL goose (di-embed)
├── web/                 # sumber telemetri browser (OpenTelemetry Web + esbuild)
├── scripts/gen-env.sh   # dipakai `make env`
├── Makefile
├── Dockerfile           # stage: node (bundle otel.js) -> go build -> distroless
├── docker-compose.yml   # app, postgres, redis, caddy, jaeger
├── Caddyfile            # TLS internal, akses lewat IP, proxy /otel -> Jaeger
├── jaeger.yaml          # config Jaeger v2 (UI di 0.0.0.0:16686)
└── .env.example
```

## Route

| Method & path | Akses |
|---|---|
| `GET /login`, `POST /login` | publik (rate limit 10/15 menit per IP) |
| `POST /logout` | login |
| `GET /tickets` | client: tiket sendiri; admin: semua + filter `?status=&client=` |
| `GET /tickets/new`, `POST /tickets` | client |
| `GET /tickets/{id}` | pemilik atau admin, selain itu 404 |
| `POST /tickets/{id}/replies` | pemilik atau admin, selain itu 404 |
| `POST /tickets/{id}/status` | admin, selain itu 404 |
| `GET /healthz` | publik (liveness) |
| `POST /otel/v1/traces` | publik; ditangani Caddy (diteruskan ke Jaeger), tidak sampai ke app |

## Perintah make

| Perintah | Fungsi |
|---|---|
| `make env` | Membuat `.env` dari `.env.example`: IP server (`hostname -I`, atau `IP=...`), `JWT_ISSUER=https://<IP>`, dan password DB/Redis, `JWT_SECRET`, `CSRF_KEY` acak. Menolak menimpa `.env` yang sudah ada, karena password DB tersimpan di volume Postgres. |
| `make up` | `docker compose build`, menyalakan postgres/redis/jaeger sampai healthy, menjalankan migrasi (`/app/migrate up`, aman diulang), lalu menyalakan app dan caddy sampai healthy. |
| `make down` | `docker compose down`. Volume (database, Redis, CA Caddy) tetap ada. |
| `make restart` | Sama dengan `make down && make up`. |
| `make user EMAIL=... NAME=... ROLE=client\|admin` | Membuat akun; password diminta interaktif. |
| `make ca-cert` | Menyalin root CA Caddy ke `./caddy-root.crt` untuk di-import ke browser/OS. CA disimpan di volume `caddy_data`, jadi tetap sama setelah restart. |
| `make ps` / `make logs [S=app]` | Status/health dan log. |
| `make migrate` | Hanya menjalankan migrasi. |
| `make test` | `go test ./...` (tanpa Postgres/Redis/Docker). |
| `make web` | Build `otel.js` lokal (butuh Node), hanya untuk menjalankan server tanpa Docker. |

Membuat user dari skrip (password lewat stdin):

```bash
printf '%s\n' "$PASSWORD" | docker compose exec -T app /app/create-user --email a@b.com --name "A" --role client
```

### Test

```bash
go test ./...          # tanpa Postgres/Redis (miniredis + SQLite in-memory)
go test -race ./...
```

Cakupan test:
- `internal/auth/token_test.go`: JWT valid; penolakan `alg=none`, HS512, secret lain, iss/aud salah, tanpa `exp`, kedaluwarsa, salah jenis token.
- `internal/auth/session_test.go`: login, rotasi refresh token, reuse yang mencabut **semua** sesi user, logout, user yang dihapus, dua rotasi bersamaan (hanya satu yang menang), dan rate limit.
- `internal/ticket/ticket_test.go`: client tidak bisa melihat atau membalas tiket orang lain (`ErrNotFound`, tanpa ada yang tertulis), client tidak bisa mengubah status, cakupan daftar tiket, dan escape wildcard LIKE.

## Deploy ke VPS / server dengan IP private

1. Pasang Docker Engine + Compose plugin dan `make`. Buka firewall untuk 22, 80, dan 443 (TCP) serta 443/UDP. Port 16686 (Jaeger UI) hanya untuk jaringan private, lihat bagian keamanan di atas.
   Catatan: port yang di-publish Docker melewati aturan ufw. Itu sebabnya Postgres dan Redis berada di jaringan `internal` dan tidak di-publish sama sekali.
2. Kode dan konfigurasi:
   ```bash
   git clone <repo> /opt/ticketing && cd /opt/ticketing
   make env                 # atau: make env IP=10.0.0.5
   make up
   make user EMAIL=admin@domain NAME="Admin" ROLE=admin
   make ps                  # semua service harus "healthy"
   ```
3. Import root CA Caddy agar browser tidak memberi peringatan: `make ca-cert`, lalu import `caddy-root.crt` sebagai *Trusted Root Certification Authority* di OS/browser klien.
4. **Update versi baru** (backup dulu, baru migrasi):
   ```bash
   git pull
   docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"' | gzip > "backup-$(date +%F).sql.gz"
   make down && make up
   ```
   Rollback: checkout commit sebelumnya, lalu `make down && make up`. Jika migrasi perlu dibatalkan, jalankan `docker compose run --rm --entrypoint /app/migrate app down` (satu langkah) atau pulihkan dari backup.
5. **Backup rutin**: jadwalkan `pg_dump` seperti di atas dengan cron, lalu simpan di luar server. Data Redis (sesi) tidak perlu di-backup; kalau hilang, user cukup login ulang.
6. **Pindah ke domain publik + Let's Encrypt** (opsional): set `SITE_ADDRESS=tiket.example.com`, `JWT_ISSUER=https://tiket.example.com`, dan `CADDY_TLS=admin@example.com` di `.env`, arahkan DNS ke server, lalu `make down && make up`.

### Operasional

- Log terstruktur JSON (slog) di production: `make logs S=app`. Salin `trace_id` dari log lalu cari di Jaeger UI (kotak "Lookup by Trace ID").
- Mencabut semua sesi satu user secara manual: `docker compose exec redis sh -c 'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli SMEMBERS user_sess:<id>'`, lalu hapus `sess:<sid>` untuk tiap sid yang muncul. Cara lain, ganti `JWT_SECRET` untuk mencabut semua sesi semua user.
- Mengganti role user berlaku paling lambat dalam 15 menit, karena data user dibaca ulang dari database pada setiap rotasi refresh token.
