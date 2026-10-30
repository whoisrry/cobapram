package auth

import (
	"net/http"
	"time"
)

// CookieNames mengembalikan nama cookie access dan refresh. Dengan Secure
// aktif dipakai prefix "__Host-", yang membuat browser menolak cookie
// tersebut jika tidak Secure, memiliki Domain, atau Path selain "/".
func CookieNames(secure bool) (access, refresh string) {
	if secure {
		return "__Host-access", "__Host-refresh"
	}
	return "access", "refresh"
}

// SetCookies menyimpan kedua token di cookie httpOnly, SameSite=Lax.
//
// Lax dipilih (bukan Strict) supaya user yang membuka tautan tiket dari luar
// aplikasi (misalnya dari chat) tetap dianggap login. Perlindungan CSRF untuk
// POST ditangani oleh token CSRF, bukan hanya SameSite.
func SetCookies(w http.ResponseWriter, t Tokens, secure bool) {
	accessName, refreshName := CookieNames(secure)
	http.SetCookie(w, newCookie(accessName, t.Access, t.AccessExpiry, secure))
	http.SetCookie(w, newCookie(refreshName, t.Refresh, t.RefreshExpiry, secure))
}

func ClearCookies(w http.ResponseWriter, secure bool) {
	accessName, refreshName := CookieNames(secure)
	for _, name := range []string{accessName, refreshName} {
		c := newCookie(name, "", time.Unix(0, 0), secure)
		c.MaxAge = -1
		http.SetCookie(w, c)
	}
}

func newCookie(name, value string, expires time.Time, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}
