// Package user berisi pembuatan akun. Dipakai bersama oleh CLI create-user
// dan form "Tambah client" di admin panel, sehingga aturan validasinya sama.
package user

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/whoisrry/cobapram/internal/auth"
	"github.com/whoisrry/cobapram/internal/models"
)

// InputError adalah kesalahan input yang aman ditampilkan ke pengguna.
type InputError struct{ Msg string }

func (e *InputError) Error() string { return e.Msg }

var ErrEmailTaken = &InputError{"Email sudah terdaftar."}

type NewUser struct {
	Name     string
	Email    string
	Password string
	Role     models.Role
}

// Create memvalidasi input, meng-hash password dengan bcrypt, lalu menyimpan
// user. Email yang sudah terdaftar dideteksi dari constraint UNIQUE di
// database (bukan cek terlebih dahulu), sehingga aman dari race condition.
func Create(ctx context.Context, db *gorm.DB, in NewUser) (models.User, error) {
	if err := Validate(in); err != nil {
		return models.User{}, err
	}
	u := models.User{
		Name:  strings.TrimSpace(in.Name),
		Email: auth.NormalizeEmail(in.Email),
		Role:  in.Role,
	}
	hash, err := auth.HashPassword(in.Password)
	if errors.Is(err, auth.ErrPasswordLength) {
		return models.User{}, &InputError{"Password minimal 12 karakter dan maksimal 72 byte."}
	}
	if err != nil {
		return models.User{}, err
	}
	u.PasswordHash = hash

	err = db.WithContext(ctx).Create(&u).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return models.User{}, ErrEmailTaken
	}
	return u, err
}

// Validate memeriksa nama, email, dan role (tanpa password). Dipisah agar CLI
// bisa gagal lebih awal sebelum meminta password.
func Validate(in NewUser) error {
	name := strings.TrimSpace(in.Name)
	email := auth.NormalizeEmail(in.Email)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return &InputError{"Nama wajib diisi, maksimal 100 karakter."}
	}
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email || len(email) > 254 {
		return &InputError{"Email tidak valid."}
	}
	if !in.Role.Valid() {
		return &InputError{"Role harus client atau admin."}
	}
	return nil
}
