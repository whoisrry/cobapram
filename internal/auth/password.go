package auth

import (
	"errors"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost        = 12
	MinPasswordLength = 12
	// bcrypt hanya memproses 72 byte pertama; password lebih panjang ditolak
	// agar tidak ada bagian password yang diam-diam diabaikan.
	MaxPasswordBytes = 72
)

var ErrPasswordLength = errors.New("password harus 12 karakter atau lebih dan maksimal 72 byte")

func HashPassword(password string) (string, error) {
	if len([]rune(password)) < MinPasswordLength || len(password) > MaxPasswordBytes {
		return "", ErrPasswordLength
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	return string(h), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// dummyHash dipakai saat email tidak ditemukan, supaya waktu respons login
// sama dengan saat email ada (mencegah enumerasi email lewat timing).
var dummyHash = sync.OnceValue(func() string {
	h, _ := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcryptCost)
	return string(h)
})
