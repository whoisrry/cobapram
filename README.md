# cobapram — Support Ticketing

Aplikasi support ticketing ringan: client membuat dan membalas tiket miliknya; admin melihat semua tiket, memfilter, membalas, dan mengubah status. Server-rendered (MPA) dengan Go `net/http` + `html/template`, PostgreSQL (GORM), Redis, JWT, Docker Compose, dan Caddy. Tidak ada JavaScript sama sekali.

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
| Tag image dikunci ke versi (`postgres:17.6-alpine`, `redis:7.4.5-alpine`, `caddy:2.10.2-alpine`, `golang:1.27.3-alpine`) | Periksa versi patch terbaru sebelum deploy. Untuk reproducibility penuh, kunci juga ke digest (`image@sha256:...`). |

## Keputusan penting (ringkas)

- **JWT HS256.** Hanya satu service yang menandatangani dan memverifikasi token, jadi kunci asimetris (EdDSA) tidak memberi manfaat. Parser mengunci algoritma ke HS256 (menolak `none`, HS512, RS256, dll.), mewajibkan `exp`, dan memeriksa `iss` serta `aud`. Claim `token_use` mencegah refresh token dipakai sebagai access token, dan sebaliknya.
- **Refresh token rotation + reuse detection** (`internal/auth/session.go`). Rotasi dijalankan oleh skrip Lua di Redis secara atomik: token lama dihapus dan ditandai `rt_used:{jti}`, lalu token baru disimpan. Jika token yang sudah ditandai dipakai lagi, **semua sesi user dicabut**. Jika dua request memakai token yang sama secara bersamaan, hanya satu yang berhasil.
- **Refresh transparan.** Karena MPA tanpa JavaScript, middleware `RequireAuth` merotasi refresh token sendiri saat access token kedaluwarsa, lalu memasang cookie baru pada response yang sama.
- **Cookie** `__Host-access` / `__Host-refresh`: `HttpOnly`, `Secure`, `SameSite=Lax`. Lax dipilih (bukan Strict) agar tautan tiket yang dibuka dari luar aplikasi tetap dalam keadaan login. Serangan CSRF ditangani token gorilla/csrf di semua POST, termasuk login dan logout.
- **Kepemilikan di klausa WHERE** (`internal/ticket/ticket.go`). Semua query tiket client memakai `WHERE tickets.user_id = ?`. Tiket orang lain menghasilkan `ErrNotFound` yang sama dengan tiket yang tidak ada, sehingga responsnya **404**. Balasan memakai `UPDATE ... WHERE id = ? AND user_id = ?` di dalam transaksi; jika 0 baris terdampak, tidak ada data yang ditulis.
- **Tanpa N+1.** Detail tiket selalu 3 query (tiket JOIN pemilik, balasan, penulis balasan), berapa pun jumlah balasannya. Daftar tiket 1 query (`Joins("User")`) tanpa kolom `body`. Kolom user yang dimuat hanya `id, name, role`, jadi `password_hash` tidak ikut terambil.
- **Setiap query** memakai `WithContext` dengan timeout `DB_QUERY_TIMEOUT`. Connection pool diatur dari `DB_MAX_OPEN_CONNS`, `DB_MAX_IDLE_CONNS`, dan `DB_CONN_MAX_LIFETIME`.
- **Tidak memakai soft delete.** Dengan `gorm.DeletedAt`, GORM otomatis menambahkan `WHERE deleted_at IS NULL` ke setiap query model tersebut, termasuk Preload dan Joins. Akibatnya, index perlu mencakup kondisi itu (partial index `WHERE deleted_at IS NULL`), constraint `UNIQUE(email)` akan bentrok dengan baris yang sudah "dihapus", dan `ON DELETE CASCADE` tidak pernah terpicu karena baris tidak benar-benar dihapus. Karena aplikasi ini tidak punya fitur hapus, soft delete tidak dipakai.
- **Login** memakai pesan generik "Email atau password salah". Saat email tidak ditemukan, bcrypt tetap dijalankan terhadap hash dummy agar waktu respons sama (mencegah enumerasi email lewat timing).
- **Header keamanan**: CSP ketat (`default-src 'none'`, CSS hanya dari `/static`), `X-Frame-Options: DENY`, `Referrer-Policy: same-origin`, `X-Content-Type-Options: nosniff`, dan HSTS di production.

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
│   └── templates/       # html/template + static/app.css (di-embed)
├── migrations/          # file SQL goose (di-embed)
├── Dockerfile
├── docker-compose.yml
├── Caddyfile
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

## Menjalankan secara lokal

Kebutuhan: Docker dengan Compose v2.

```bash
cp .env.example .env
# Edit .env:
#   SITE_ADDRESS=localhost
#   JWT_ISSUER=https://localhost
#   DB_PASSWORD, REDIS_PASSWORD, JWT_SECRET, CSRF_KEY -> isi dengan: openssl rand -hex 32
#   (JWT_SECRET dan CSRF_KEY harus berbeda)

docker compose build
docker compose up -d postgres redis
docker compose run --rm --entrypoint /app/migrate app up

docker compose up -d

# Buat akun (password diminta secara interaktif, tidak masuk shell history)
docker compose exec app /app/create-user --email admin@example.com --name "Admin" --role admin
docker compose exec app /app/create-user --email client@example.com --name "Client A" --role client
```

Buka https://localhost. Caddy menerbitkan sertifikat dari CA internalnya untuk `localhost`, jadi browser akan memberi peringatan sertifikat; terima peringatan itu untuk pengujian lokal.

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

## Deploy ke VPS

1. **Siapkan server** (Ubuntu/Debian): pasang Docker Engine + Compose plugin. Buka firewall hanya untuk 22, 80, dan 443 (TCP) serta 443/UDP:
   ```bash
   ufw allow OpenSSH && ufw allow 80/tcp && ufw allow 443 && ufw enable
   ```
   Catatan: port yang di-publish Docker melewati aturan ufw. Itu sebabnya hanya Caddy yang mem-publish port, sedangkan Postgres dan Redis berada di jaringan `internal`.
2. **DNS**: arahkan record A/AAAA domain (misalnya `tiket.example.com`) ke IP VPS. Caddy membutuhkan port 80/443 terbuka untuk menerbitkan sertifikat Let's Encrypt.
3. **Kode & konfigurasi**:
   ```bash
   git clone <repo> /opt/ticketing && cd /opt/ticketing
   cp .env.example .env && chmod 600 .env
   # APP_ENV=production, SITE_ADDRESS=tiket.example.com, JWT_ISSUER=https://tiket.example.com,
   # COOKIE_SECURE=true, dan semua secret diisi dengan: openssl rand -hex 32
   ```
4. **Build, migrasi, jalankan**:
   ```bash
   docker compose build
   docker compose up -d postgres redis
   docker compose run --rm --entrypoint /app/migrate app up
   docker compose up -d
   docker compose ps          # semua service harus "healthy"
   docker compose exec app /app/create-user --email admin@domain --name "Admin" --role admin
   ```
5. **Update versi baru** (backup dulu, baru migrasi):
   ```bash
   git pull
   docker compose build app
   docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"' | gzip > "backup-$(date +%F).sql.gz"
   docker compose run --rm --entrypoint /app/migrate app up
   docker compose up -d app
   ```
   Rollback: checkout commit sebelumnya, build ulang, lalu `docker compose up -d app`. Jika migrasi perlu dibatalkan, jalankan `migrate down` (satu langkah) atau pulihkan dari backup.
6. **Backup rutin**: jadwalkan `pg_dump` seperti di atas dengan cron, lalu simpan di luar VPS. Data Redis (sesi) tidak perlu di-backup; kalau hilang, user cukup login ulang.

### Operasional

- Log terstruktur JSON (slog) di production: `docker compose logs -f app`.
- Mencabut semua sesi satu user secara manual: `docker compose exec redis sh -c 'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli SMEMBERS user_sess:<id>'`, lalu hapus `sess:<sid>` untuk tiap sid yang muncul. Cara lain, ganti `JWT_SECRET` untuk mencabut semua sesi semua user.
- Mengganti role user berlaku paling lambat dalam 15 menit, karena data user dibaca ulang dari database pada setiap rotasi refresh token.
