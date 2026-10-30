package user

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/whoisrry/cobapram/internal/auth"
	"github.com/whoisrry/cobapram/internal/models"
)

// newTestDB memakai SQLite in-memory. AutoMigrate dan index di sini hanya
// untuk test; skema produksi dikelola goose.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard, TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX users_email_key ON users (email)").Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCreate(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	u, err := Create(ctx, db, NewUser{Name: "  Client A ", Email: " Client@Example.COM ", Password: "password-awal-123", Role: models.RoleClient})
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == 0 || u.Name != "Client A" || u.Email != "client@example.com" || u.Role != models.RoleClient {
		t.Fatalf("user tidak sesuai: %+v", u)
	}
	if u.PasswordHash == "password-awal-123" || !auth.CheckPassword(u.PasswordHash, "password-awal-123") {
		t.Fatal("password tidak di-hash dengan benar")
	}

	_, err = Create(ctx, db, NewUser{Name: "Lain", Email: "CLIENT@example.com", Password: "password-awal-123", Role: models.RoleClient})
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("email duplikat: err = %v, want ErrEmailTaken", err)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	db := newTestDB(t)
	valid := NewUser{Name: "Client", Email: "c@example.com", Password: "password-awal-123", Role: models.RoleClient}

	cases := map[string]func(*NewUser){
		"nama kosong":          func(u *NewUser) { u.Name = "   " },
		"nama terlalu panjang": func(u *NewUser) { u.Name = strings.Repeat("a", 101) },
		"email tanpa @":        func(u *NewUser) { u.Email = "bukan-email" },
		"email dengan nama":    func(u *NewUser) { u.Email = "Budi <b@example.com>" },
		"role tidak dikenal":   func(u *NewUser) { u.Role = "superadmin" },
		"password pendek":      func(u *NewUser) { u.Password = "pendek" },
		"password > 72 byte":   func(u *NewUser) { u.Password = strings.Repeat("x", 73) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := valid
			mutate(&in)
			_, err := Create(context.Background(), db, in)
			var inputErr *InputError
			if !errors.As(err, &inputErr) {
				t.Fatalf("err = %v, want *InputError", err)
			}
		})
	}

	var n int64
	db.Model(&models.User{}).Count(&n)
	if n != 0 {
		t.Fatalf("input tidak valid tetap tersimpan (%d user)", n)
	}
}
