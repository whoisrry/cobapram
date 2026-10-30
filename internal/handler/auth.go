package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/whoisrry/cobapram/internal/auth"
	mw "github.com/whoisrry/cobapram/internal/middleware"
)

const msgInvalidLogin = "Email atau password salah"

func (h *Handler) loginForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	h.render(w, r, http.StatusOK, "login.html", h.newView(r, "Masuk"))
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	v := h.newView(r, "Masuk")
	if err := r.ParseForm(); err != nil {
		v.Error = msgInvalidLogin
		h.render(w, r, http.StatusBadRequest, "login.html", v)
		return
	}
	email := auth.NormalizeEmail(r.PostForm.Get("email"))
	password := r.PostForm.Get("password")
	v.Form.Email = email

	allowed, retryAfter, err := h.limiter.Allow(r.Context(), h.clientIP(r))
	if err != nil {
		h.serverError(w, r, fmt.Errorf("rate limiter: %w", err))
		return
	}
	if !allowed {
		h.log.Warn("login diblokir rate limit", "ip", h.clientIP(r))
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		v.Error = fmt.Sprintf("Terlalu banyak percobaan masuk. Coba lagi dalam %d menit.", retryMinutes(retryAfter))
		h.render(w, r, http.StatusTooManyRequests, "login.html", v)
		return
	}

	if email == "" || password == "" || len(password) > auth.MaxPasswordBytes {
		v.Error = msgInvalidLogin
		h.render(w, r, http.StatusUnauthorized, "login.html", v)
		return
	}

	tokens, p, err := h.auth.Login(r.Context(), email, password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		v.Error = msgInvalidLogin
		h.render(w, r, http.StatusUnauthorized, "login.html", v)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.log.Info("login berhasil", "user_id", p.UserID)
	auth.SetCookies(w, tokens, h.cfg.CookieSecure)
	http.Redirect(w, r, "/tickets", http.StatusSeeOther)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	p, _ := mw.CurrentUser(r.Context())
	if err := h.auth.Logout(r.Context(), p); err != nil {
		h.serverError(w, r, err)
		return
	}
	auth.ClearCookies(w, h.cfg.CookieSecure)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
