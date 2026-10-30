package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/whoisrry/cobapram/internal/models"
)

func newTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return mr, rdb
}

// newTestService memakai SQLite in-memory. AutoMigrate di sini hanya untuk
// test; skema produksi dikelola goose.
func newTestService(t *testing.T) (*Service, *miniredis.Miniredis) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := gdb.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	mr, rdb := newTestRedis(t)
	return &Service{
		DB:           gdb,
		Tokens:       newTestIssuer(),
		Sessions:     NewSessionStore(rdb, 7*24*time.Hour),
		QueryTimeout: time.Second,
	}, mr
}

func createTestUser(t *testing.T, s *Service, email, password string) models.User {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	u := models.User{Name: "Test", Email: email, PasswordHash: hash, Role: models.RoleClient}
	if err := s.DB.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLogin(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	createTestUser(t, s, "client@test.id", "password-yang-kuat")

	if _, _, err := s.Login(ctx, "client@test.id", "salah-password-nya"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("password salah: err = %v", err)
	}
	if _, _, err := s.Login(ctx, "tidakada@test.id", "password-yang-kuat"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("email tidak ada: err = %v", err)
	}
	tok, p, err := s.Login(ctx, " CLIENT@test.id ", "password-yang-kuat")
	if err != nil {
		t.Fatalf("login valid gagal: %v", err)
	}
	if _, err := s.Authenticate(ctx, tok.Access); err != nil {
		t.Fatalf("access token hasil login ditolak: %v", err)
	}
	if p.Role != models.RoleClient {
		t.Fatalf("role = %s", p.Role)
	}
}

func TestRefreshRotation(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	createTestUser(t, s, "client@test.id", "password-yang-kuat")

	t1, _, err := s.Login(ctx, "client@test.id", "password-yang-kuat")
	if err != nil {
		t.Fatal(err)
	}

	t2, _, err := s.Refresh(ctx, t1.Refresh)
	if err != nil {
		t.Fatalf("refresh pertama gagal: %v", err)
	}
	if t2.Refresh == t1.Refresh || t2.RefreshJTI == t1.RefreshJTI {
		t.Fatal("refresh token tidak dirotasi")
	}
	if t2.SID != t1.SID {
		t.Fatal("rotasi harus mempertahankan session id")
	}

	// Token baru bisa dirotasi lagi.
	t3, _, err := s.Refresh(ctx, t2.Refresh)
	if err != nil {
		t.Fatalf("refresh kedua gagal: %v", err)
	}
	if _, err := s.Authenticate(ctx, t3.Access); err != nil {
		t.Fatalf("access token hasil rotasi ditolak: %v", err)
	}
}

func TestRefreshReuseRevokesAllSessions(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	createTestUser(t, s, "client@test.id", "password-yang-kuat")

	// Dua sesi: misalnya laptop dan HP.
	laptop, _, _ := s.Login(ctx, "client@test.id", "password-yang-kuat")
	phone, _, _ := s.Login(ctx, "client@test.id", "password-yang-kuat")

	rotated, _, err := s.Refresh(ctx, laptop.Refresh)
	if err != nil {
		t.Fatal(err)
	}

	// Refresh token lama dipakai lagi (misalnya dicuri penyerang).
	if _, _, err := s.Refresh(ctx, laptop.Refresh); !errors.Is(err, ErrRefreshReused) {
		t.Fatalf("reuse: err = %v, want ErrRefreshReused", err)
	}

	// Semua sesi user harus dicabut, termasuk sesi lain dan token hasil rotasi.
	for name, tok := range map[string]Tokens{"laptop rotated": rotated, "phone": phone} {
		if _, err := s.Authenticate(ctx, tok.Access); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("%s: access masih berlaku setelah reuse, err = %v", name, err)
		}
		if _, _, err := s.Refresh(ctx, tok.Refresh); err == nil {
			t.Errorf("%s: refresh masih berlaku setelah reuse", name)
		}
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	createTestUser(t, s, "client@test.id", "password-yang-kuat")

	tok, p, _ := s.Login(ctx, "client@test.id", "password-yang-kuat")
	if err := s.Logout(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, tok.Access); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("access setelah logout: err = %v", err)
	}
	if _, _, err := s.Refresh(ctx, tok.Refresh); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("refresh setelah logout: err = %v, want ErrRefreshInvalid", err)
	}
}

func TestRefreshRejectsDeletedUser(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	u := createTestUser(t, s, "client@test.id", "password-yang-kuat")
	tok, _, _ := s.Login(ctx, "client@test.id", "password-yang-kuat")

	if err := s.DB.Delete(&models.User{}, u.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Refresh(ctx, tok.Refresh); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("err = %v, want ErrRefreshInvalid", err)
	}
}

func TestRotateConcurrentOnlyOneWins(t *testing.T) {
	_, rdb := newTestRedis(t)
	store := NewSessionStore(rdb, time.Hour)
	ctx := context.Background()
	if err := store.Create(ctx, 1, "sid", "jti-0"); err != nil {
		t.Fatal(err)
	}

	results := make(chan error, 2)
	for _, newJTI := range []string{"jti-a", "jti-b"} {
		go func() { results <- store.Rotate(ctx, 1, "sid", "jti-0", newJTI) }()
	}
	var ok, reused int
	for range 2 {
		switch err := <-results; {
		case err == nil:
			ok++
		case errors.Is(err, ErrRefreshReused):
			reused++
		default:
			t.Fatalf("error tak terduga: %v", err)
		}
	}
	if ok != 1 || reused != 1 {
		t.Fatalf("ok=%d reused=%d, want 1 dan 1", ok, reused)
	}
}

func TestLoginLimiter(t *testing.T) {
	mr, rdb := newTestRedis(t)
	l := NewLoginLimiter(rdb, 10, 15*time.Minute)
	ctx := context.Background()

	for i := 1; i <= 10; i++ {
		if ok, _, err := l.Allow(ctx, "10.0.0.1"); err != nil || !ok {
			t.Fatalf("percobaan ke-%d ditolak (err=%v)", i, err)
		}
	}
	ok, retry, _ := l.Allow(ctx, "10.0.0.1")
	if ok {
		t.Fatal("percobaan ke-11 seharusnya ditolak")
	}
	if retry <= 0 || retry > 15*time.Minute {
		t.Fatalf("retryAfter = %v", retry)
	}
	if ok, _, _ := l.Allow(ctx, "10.0.0.2"); !ok {
		t.Fatal("IP lain tidak boleh ikut terblokir")
	}

	mr.FastForward(15*time.Minute + time.Second)
	if ok, _, _ := l.Allow(ctx, "10.0.0.1"); !ok {
		t.Fatal("setelah window berakhir, percobaan harus diizinkan lagi")
	}
}
