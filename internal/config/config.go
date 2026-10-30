// Package config membaca seluruh konfigurasi aplikasi dari environment variable.
package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type DBConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	Name            string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	QueryTimeout    time.Duration
}

// DSN membangun URL koneksi PostgreSQL. url.URL meng-escape karakter khusus
// di password sehingga password acak apa pun aman dipakai.
func (c DBConfig) DSN() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, c.Password),
		Host:     fmt.Sprintf("%s:%d", c.Host, c.Port),
		Path:     "/" + c.Name,
		RawQuery: url.Values{"sslmode": {c.SSLMode}}.Encode(),
	}
	return u.String()
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type Config struct {
	Env  string // "development" atau "production"
	Addr string

	DB    DBConfig
	Redis RedisConfig

	JWTSecret   []byte
	JWTIssuer   string
	JWTAudience string
	AccessTTL   time.Duration
	RefreshTTL  time.Duration

	CSRFKey []byte // selalu 32 byte (diturunkan dengan SHA-256 dari CSRF_KEY)

	// CookieSecure=false hanya untuk development tanpa HTTPS (tanpa Caddy).
	CookieSecure bool
	// TrustProxyHeaders=true jika aplikasi hanya bisa diakses lewat Caddy,
	// sehingga X-Forwarded-For dapat dipercaya untuk IP client.
	TrustProxyHeaders bool

	LoginRateLimit  int
	LoginRateWindow time.Duration
}

func (c Config) IsProduction() bool { return c.Env == "production" }

// Load membaca konfigurasi lengkap untuk server.
func Load() (Config, error) {
	var errs []error
	r := reader{errs: &errs}

	db, dbErr := LoadDB()
	if dbErr != nil {
		errs = append(errs, dbErr)
	}

	cfg := Config{
		Env:  r.str("APP_ENV", "production"),
		Addr: r.str("HTTP_ADDR", ":8080"),
		DB:   db,
		Redis: RedisConfig{
			Addr:     r.str("REDIS_ADDR", "localhost:6379"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       r.int("REDIS_DB", 0),
		},
		JWTSecret:         []byte(r.required("JWT_SECRET")),
		JWTIssuer:         r.required("JWT_ISSUER"),
		JWTAudience:       r.required("JWT_AUDIENCE"),
		AccessTTL:         r.duration("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTTL:        r.duration("REFRESH_TOKEN_TTL", 7*24*time.Hour),
		CookieSecure:      r.bool("COOKIE_SECURE", true),
		TrustProxyHeaders: r.bool("TRUST_PROXY_HEADERS", false),
		LoginRateLimit:    r.int("LOGIN_RATE_LIMIT", 10),
		LoginRateWindow:   r.duration("LOGIN_RATE_WINDOW", 15*time.Minute),
	}

	if cfg.Env != "production" && cfg.Env != "development" {
		errs = append(errs, fmt.Errorf("APP_ENV harus 'production' atau 'development', bukan %q", cfg.Env))
	}
	if len(cfg.JWTSecret) > 0 && len(cfg.JWTSecret) < 32 {
		errs = append(errs, errors.New("JWT_SECRET minimal 32 karakter (contoh: openssl rand -hex 32)"))
	}
	csrfKey := r.required("CSRF_KEY")
	if csrfKey != "" && len(csrfKey) < 32 {
		errs = append(errs, errors.New("CSRF_KEY minimal 32 karakter (contoh: openssl rand -hex 32)"))
	}
	if csrfKey != "" && csrfKey == string(cfg.JWTSecret) {
		errs = append(errs, errors.New("CSRF_KEY dan JWT_SECRET harus berbeda"))
	}
	sum := sha256.Sum256([]byte(csrfKey))
	cfg.CSRFKey = sum[:]

	if cfg.IsProduction() && !cfg.CookieSecure {
		errs = append(errs, errors.New("COOKIE_SECURE=false tidak diizinkan saat APP_ENV=production"))
	}
	if cfg.AccessTTL >= cfg.RefreshTTL {
		errs = append(errs, errors.New("ACCESS_TOKEN_TTL harus lebih pendek dari REFRESH_TOKEN_TTL"))
	}
	if cfg.LoginRateLimit < 1 {
		errs = append(errs, errors.New("LOGIN_RATE_LIMIT minimal 1"))
	}

	return cfg, errors.Join(errs...)
}

// LoadDB hanya membaca konfigurasi database. Dipakai juga oleh CLI
// (create-user, migrate) yang tidak butuh JWT/Redis.
func LoadDB() (DBConfig, error) {
	var errs []error
	r := reader{errs: &errs}
	c := DBConfig{
		Host:            r.str("DB_HOST", "localhost"),
		Port:            r.int("DB_PORT", 5432),
		User:            r.required("DB_USER"),
		Password:        r.required("DB_PASSWORD"),
		Name:            r.required("DB_NAME"),
		SSLMode:         r.str("DB_SSLMODE", "disable"),
		MaxOpenConns:    r.int("DB_MAX_OPEN_CONNS", 20),
		MaxIdleConns:    r.int("DB_MAX_IDLE_CONNS", 10),
		ConnMaxLifetime: r.duration("DB_CONN_MAX_LIFETIME", 30*time.Minute),
		QueryTimeout:    r.duration("DB_QUERY_TIMEOUT", 5*time.Second),
	}
	if c.MaxIdleConns > c.MaxOpenConns {
		errs = append(errs, errors.New("DB_MAX_IDLE_CONNS tidak boleh lebih besar dari DB_MAX_OPEN_CONNS"))
	}
	return c, errors.Join(errs...)
}

// reader mengumpulkan semua error parsing agar operator melihat semua
// kesalahan konfigurasi sekaligus, bukan satu per satu.
type reader struct{ errs *[]error }

func (r reader) str(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func (r reader) required(key string) string {
	v := os.Getenv(key)
	if v == "" {
		*r.errs = append(*r.errs, fmt.Errorf("%s wajib diisi", key))
	}
	return v
}

func (r reader) int(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		*r.errs = append(*r.errs, fmt.Errorf("%s bukan angka: %q", key, v))
	}
	return n
}

func (r reader) bool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		*r.errs = append(*r.errs, fmt.Errorf("%s bukan boolean: %q", key, v))
	}
	return b
}

func (r reader) duration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		*r.errs = append(*r.errs, fmt.Errorf("%s bukan durasi valid (contoh: 15m): %q", key, v))
	}
	return d
}
