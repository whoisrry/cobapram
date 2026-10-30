package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/whoisrry/cobapram/internal/auth"
	"github.com/whoisrry/cobapram/internal/models"
)

type ctxKey struct{}

// CurrentUser mengambil Principal yang dipasang oleh RequireAuth.
func CurrentUser(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(auth.Principal)
	return p, ok
}

type Authenticator struct {
	Auth         *auth.Service
	CookieSecure bool
	Log          *slog.Logger
}

// RequireAuth memastikan request memiliki sesi valid. Karena aplikasi MPA
// tanpa JavaScript, refresh dilakukan transparan di sini: jika access token
// tidak ada atau kedaluwarsa, refresh token dirotasi dan cookie baru dipasang
// pada response yang sama.
func (a *Authenticator) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accessName, refreshName := auth.CookieNames(a.CookieSecure)
		ctx := r.Context()

		if c, err := r.Cookie(accessName); err == nil {
			p, err := a.Auth.Authenticate(ctx, c.Value)
			if err == nil {
				a.serve(w, r, next, p)
				return
			}
			if !errors.Is(err, auth.ErrUnauthenticated) {
				a.fail(w, err)
				return
			}
			// Access token tidak valid/kedaluwarsa: lanjut coba refresh token.
		}

		c, err := r.Cookie(refreshName)
		if err != nil {
			a.toLogin(w, r)
			return
		}
		tokens, p, err := a.Auth.Refresh(ctx, c.Value)
		switch {
		case err == nil:
			auth.SetCookies(w, tokens, a.CookieSecure)
			a.serve(w, r, next, p)
		case errors.Is(err, auth.ErrRefreshReused):
			a.Log.Warn("refresh token dipakai ulang, semua sesi user dicabut")
			a.toLogin(w, r)
		case errors.Is(err, auth.ErrRefreshInvalid):
			a.toLogin(w, r)
		default:
			a.fail(w, err)
		}
	})
}

func (a *Authenticator) serve(w http.ResponseWriter, r *http.Request, next http.Handler, p auth.Principal) {
	// Halaman berisi data pribadi tidak boleh disimpan di cache browser/proxy.
	w.Header().Set("Cache-Control", "no-store")
	next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
}

func (a *Authenticator) toLogin(w http.ResponseWriter, r *http.Request) {
	auth.ClearCookies(w, a.CookieSecure)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// fail dipakai saat Redis/DB bermasalah: jangan logout user, cukup 503.
func (a *Authenticator) fail(w http.ResponseWriter, err error) {
	a.Log.Error("autentikasi gagal karena error internal", "error", err)
	http.Error(w, "Layanan sedang bermasalah, coba lagi sebentar.", http.StatusServiceUnavailable)
}

// RequireRole membalas 404 (bukan 403) untuk role lain, konsisten dengan
// kebijakan tidak membocorkan keberadaan resource.
func RequireRole(role models.Role, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := CurrentUser(r.Context())
		if !ok || p.Role != role {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
