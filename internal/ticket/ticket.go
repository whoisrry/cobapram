// Package ticket berisi query tiket dan balasan. Setiap fungsi menerima
// Viewer, dan untuk client kepemilikan selalu diperiksa di klausa WHERE,
// bukan setelah data diambil.
package ticket

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/whoisrry/cobapram/internal/models"
)

// ErrNotFound dikembalikan baik saat tiket tidak ada maupun saat tiket milik
// client lain, supaya keberadaan ID tiket orang lain tidak bisa ditebak.
var ErrNotFound = errors.New("tiket tidak ditemukan")

const PageSize = 25

type Viewer struct {
	UserID int64
	Role   models.Role
}

func (v Viewer) IsAdmin() bool { return v.Role == models.RoleAdmin }

// publicUser memilih kolom user yang dibutuhkan halaman saja, sehingga
// password_hash tidak ikut dimuat lewat JOIN/Preload.
func publicUser(db *gorm.DB) *gorm.DB {
	return db.Select("id", "name", "role")
}

// scope membatasi query tiket sesuai hak akses viewer.
func scope(db *gorm.DB, v Viewer) *gorm.DB {
	if v.IsAdmin() {
		return db
	}
	return db.Where("tickets.user_id = ?", v.UserID)
}

type ListFilter struct {
	Status     models.Status // hanya untuk admin; kosong = semua
	ClientName string        // hanya untuk admin; pencarian sebagian nama
	Page       int           // mulai dari 1
}

// List mengembalikan satu halaman tiket, terbaru di atas. hasNext bernilai
// true jika masih ada halaman berikutnya.
func List(ctx context.Context, db *gorm.DB, v Viewer, f ListFilter) (tickets []models.Ticket, hasNext bool, err error) {
	if f.Page < 1 {
		f.Page = 1
	}
	// Joins("User") memuat pemilik tiket dalam satu query (LEFT JOIN),
	// sehingga tidak ada N+1 saat menampilkan nama client.
	q := scope(db.WithContext(ctx).Joins("User", publicUser(db.Session(&gorm.Session{NewDB: true}))), v)
	if v.IsAdmin() {
		if f.Status != "" {
			q = q.Where("tickets.status = ?", f.Status)
		}
		if name := strings.TrimSpace(f.ClientName); name != "" {
			q = q.Where(`"User"."name" ILIKE ? ESCAPE '\'`, "%"+escapeLike(name)+"%")
		}
	}
	err = q.Omit("tickets.body").
		Order("tickets.updated_at DESC, tickets.id DESC").
		Limit(PageSize + 1).
		Offset((f.Page - 1) * PageSize).
		Find(&tickets).Error
	if err != nil {
		return nil, false, err
	}
	if len(tickets) > PageSize {
		return tickets[:PageSize], true, nil
	}
	return tickets, false, nil
}

// Get mengambil satu tiket beserta pemilik, balasan, dan penulis balasan.
// Total tiga query, berapa pun jumlah balasannya.
func Get(ctx context.Context, db *gorm.DB, v Viewer, id int64) (models.Ticket, error) {
	var t models.Ticket
	err := scope(db.WithContext(ctx), v).
		Joins("User", publicUser(db.Session(&gorm.Session{NewDB: true}))).
		Preload("Replies", func(db *gorm.DB) *gorm.DB {
			return db.Order("replies.created_at ASC, replies.id ASC")
		}).
		Preload("Replies.User", publicUser).
		Where("tickets.id = ?", id).
		Take(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Ticket{}, ErrNotFound
	}
	return t, err
}

func Create(ctx context.Context, db *gorm.DB, ownerID int64, subject, body string) (models.Ticket, error) {
	t := models.Ticket{UserID: ownerID, Subject: subject, Body: body, Status: models.StatusOpen}
	err := db.WithContext(ctx).Create(&t).Error
	return t, err
}

// AddReply menambah balasan dan memperbarui tickets.updated_at dalam satu
// transaksi. UPDATE dengan scope kepemilikan berfungsi sekaligus sebagai
// cek akses dan row lock: jika 0 baris terdampak, tiket tidak ada atau
// bukan milik client, dan tidak ada yang ditulis.
func AddReply(ctx context.Context, db *gorm.DB, v Viewer, ticketID int64, body string) (models.Reply, error) {
	r := models.Reply{TicketID: ticketID, UserID: v.UserID, Body: body}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := scope(tx.Model(&models.Ticket{}), v).
			Where("tickets.id = ?", ticketID).
			Update("updated_at", time.Now().UTC())
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return tx.Create(&r).Error
	})
	return r, err
}

// UpdateStatus hanya untuk admin. Pengecekan role juga dilakukan di router;
// pengecekan di sini adalah lapisan kedua.
func UpdateStatus(ctx context.Context, db *gorm.DB, v Viewer, ticketID int64, status models.Status) error {
	if !v.IsAdmin() {
		return ErrNotFound
	}
	if !status.Valid() {
		return errors.New("status tidak valid")
	}
	res := db.WithContext(ctx).Model(&models.Ticket{}).
		Where("id = ?", ticketID).
		Updates(map[string]any{"status": status, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// escapeLike meng-escape karakter wildcard LIKE agar input user
// diperlakukan sebagai teks biasa.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
