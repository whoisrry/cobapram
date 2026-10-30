package ticket

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/whoisrry/cobapram/internal/models"
)

type fixture struct {
	db          *gorm.DB
	alice, bob  Viewer
	admin       Viewer
	aliceTicket models.Ticket
	bobTicket   models.Ticket
}

// setup memakai SQLite in-memory. AutoMigrate di sini hanya untuk test;
// skema produksi dikelola goose.
func setup(t *testing.T) fixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.User{}, &models.Ticket{}, &models.Reply{}); err != nil {
		t.Fatal(err)
	}

	users := []models.User{
		{Name: "Alice", Email: "alice@test.id", Role: models.RoleClient, PasswordHash: "x"},
		{Name: "Bob", Email: "bob@test.id", Role: models.RoleClient, PasswordHash: "x"},
		{Name: "Admin", Email: "admin@test.id", Role: models.RoleAdmin, PasswordHash: "x"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	f := fixture{
		db:    db,
		alice: Viewer{UserID: users[0].ID, Role: models.RoleClient},
		bob:   Viewer{UserID: users[1].ID, Role: models.RoleClient},
		admin: Viewer{UserID: users[2].ID, Role: models.RoleAdmin},
	}
	ctx := context.Background()
	if f.aliceTicket, err = Create(ctx, db, f.alice.UserID, "Printer rusak", "Tidak bisa mencetak"); err != nil {
		t.Fatal(err)
	}
	if f.bobTicket, err = Create(ctx, db, f.bob.UserID, "Lupa password email", "Tolong reset"); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestGetOwnership(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if _, err := Get(ctx, f.db, f.alice, f.aliceTicket.ID); err != nil {
		t.Fatalf("pemilik tidak bisa melihat tiketnya: %v", err)
	}
	// Tiket milik client lain harus sama persis dengan tiket yang tidak ada.
	if _, err := Get(ctx, f.db, f.alice, f.bobTicket.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("client melihat tiket orang lain: err = %v, want ErrNotFound", err)
	}
	if _, err := Get(ctx, f.db, f.alice, 99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tiket tidak ada: err = %v, want ErrNotFound", err)
	}
	got, err := Get(ctx, f.db, f.admin, f.bobTicket.ID)
	if err != nil {
		t.Fatalf("admin tidak bisa melihat tiket: %v", err)
	}
	if got.User.Name != "Bob" {
		t.Fatalf("relasi User tidak dimuat: %+v", got.User)
	}
}

func TestAddReplyOwnership(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if _, err := AddReply(ctx, f.db, f.alice, f.bobTicket.ID, "menyusup"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("client membalas tiket orang lain: err = %v, want ErrNotFound", err)
	}
	var n int64
	f.db.Model(&models.Reply{}).Count(&n)
	if n != 0 {
		t.Fatalf("balasan tetap tersimpan padahal ditolak (%d baris)", n)
	}

	if _, err := AddReply(ctx, f.db, f.alice, f.aliceTicket.ID, "ada update?"); err != nil {
		t.Fatalf("pemilik gagal membalas: %v", err)
	}
	if _, err := AddReply(ctx, f.db, f.admin, f.aliceTicket.ID, "sedang dicek"); err != nil {
		t.Fatalf("admin gagal membalas: %v", err)
	}

	got, err := Get(ctx, f.db, f.alice, f.aliceTicket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Replies) != 2 || got.Replies[0].User.Name != "Alice" || got.Replies[1].User.Name != "Admin" {
		t.Fatalf("balasan/penulis tidak sesuai: %+v", got.Replies)
	}
	if !got.UpdatedAt.After(f.aliceTicket.UpdatedAt) && !got.UpdatedAt.Equal(f.aliceTicket.UpdatedAt) {
		t.Fatal("updated_at tiket tidak diperbarui")
	}
}

func TestUpdateStatusAdminOnly(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if err := UpdateStatus(ctx, f.db, f.alice, f.aliceTicket.ID, models.StatusClosed); !errors.Is(err, ErrNotFound) {
		t.Fatalf("client mengubah status: err = %v, want ErrNotFound", err)
	}
	if err := UpdateStatus(ctx, f.db, f.admin, f.aliceTicket.ID, models.Status("hacked")); err == nil {
		t.Fatal("status tidak valid diterima")
	}
	if err := UpdateStatus(ctx, f.db, f.admin, 99999, models.StatusClosed); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tiket tidak ada: err = %v, want ErrNotFound", err)
	}
	if err := UpdateStatus(ctx, f.db, f.admin, f.aliceTicket.ID, models.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	got, _ := Get(ctx, f.db, f.admin, f.aliceTicket.ID)
	if got.Status != models.StatusInProgress {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestListScope(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	got, _, err := List(ctx, f.db, f.alice, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != f.aliceTicket.ID {
		t.Fatalf("client melihat tiket selain miliknya: %+v", got)
	}

	// Filter status dari client diabaikan, tidak membuka tiket orang lain.
	got, _, _ = List(ctx, f.db, f.alice, ListFilter{Status: models.StatusOpen})
	if len(got) != 1 {
		t.Fatalf("client dengan filter: %d tiket", len(got))
	}

	got, _, _ = List(ctx, f.db, f.admin, ListFilter{})
	if len(got) != 2 {
		t.Fatalf("admin melihat %d tiket, want 2", len(got))
	}
	_ = UpdateStatus(ctx, f.db, f.admin, f.bobTicket.ID, models.StatusClosed)
	got, _, _ = List(ctx, f.db, f.admin, ListFilter{Status: models.StatusClosed})
	if len(got) != 1 || got[0].ID != f.bobTicket.ID || got[0].User.Name != "Bob" {
		t.Fatalf("filter status admin: %+v", got)
	}
}

func TestEscapeLike(t *testing.T) {
	if got := escapeLike(`50%_a\b`); got != `50\%\_a\\b` {
		t.Fatalf("escapeLike = %q", got)
	}
}
